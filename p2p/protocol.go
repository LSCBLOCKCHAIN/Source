package p2p

import (
	"bytes"
	"time"
)

// Protocol represents a P2P subprotocol implementation.
type Protocol struct {
	// Name should contain the official protocol name,
	// often a three-letter word.
	Name string

	// Version should contain the version number of the protocol.
	Version uint

	// Length should contain the number of message codes used
	// by the protocol.
	Length uint64

	// Run is called in a new groutine when the protocol has been
	// negotiated with a peer. It should read and write messages from
	// rw. The Payload for each message must be fully consumed.
	//
	// The peer connection is closed when Start returns. It should return
	// any protocol-level error (such as an I/O error) that is
	// encountered.
	Run func(peer *Peer, rw MsgReadWriter) error
}

func (p Protocol) cap() Cap {
	return Cap{p.Name, p.Version}
}

const (
	baseProtocolVersion    = 2
	baseProtocolLength     = uint64(16)
	baseProtocolMaxMsgSize = 10 * 1024 * 1024
)

const (
	// devp2p message codes
	handshakeMsg = 0x00
	discMsg      = 0x01
	pingMsg      = 0x02
	pongMsg      = 0x03
	getPeersMsg  = 0x04
	peersMsg     = 0x05
)

// handshake is the structure of a handshake list.
type handshake struct {
	Version    uint64
	ID         string
	Caps       []Cap
	ListenPort uint64
	NodeID     []byte
}

func (h *handshake) String() string {
	return h.ID
}
func (h *handshake) Pubkey() []byte {
	return h.NodeID
}

// Cap is the structure of a peer capability.
type Cap struct {
	Name    string
	Version uint
}

func (cap Cap) RlpData() interface{} {
	return []interface{}{cap.Name, cap.Version}
}

type capsByName []Cap

func (cs capsByName) Len() int           { return len(cs) }
func (cs capsByName) Less(i, j int) bool { return cs[i].Name < cs[j].Name }
func (cs capsByName) Swap(i, j int)      { cs[i], cs[j] = cs[j], cs[i] }

type baseProtocol struct {
	rw   MsgReadWriter
	peer *Peer
}

func runBaseProtocol(peer *Peer, rw MsgReadWriter) error {
	bp := &baseProtocol{rw, peer}
	errc := make(chan error, 1)
	go func() { errc <- rw.WriteMsg(bp.handshakeMsg()) }()
	if err := bp.readHandshake(); err != nil {
		return err
	}
	// handle write error
	if err := <-errc; err != nil {
		return err
	}
	// run main loop
	go func() {
		for {
			if err := bp.handle(rw); err != nil {
				errc <- err
				break
			}
		}
	}()
	var lastActiveC chan time.Time
	if bp.peer.listenAddr != nil {
		lastActiveC = bp.peer.listenAddr.lastActiveC
	}
	return bp.loop(errc, lastActiveC)
}

var pingTimeout = 2 * time.Second

func (bp *baseProtocol) loop(quit <-chan error, lastActiveC chan time.Time) error {
	ping := time.NewTimer(pingTimeout)
	activity := bp.peer.activity.Subscribe(time.Time{})
	lastActive := time.Time{}
	defer ping.Stop()
	defer activity.Unsubscribe()

	getPeersTick := time.NewTicker(10 * time.Second)
	defer getPeersTick.Stop()
	err := bp.rw.EncodeMsg(getPeersMsg)

	for err == nil {
		select {
		case err = <-quit:
			return err
		case lastActiveC <- lastActive:
		case <-getPeersTick.C:
			err = bp.rw.EncodeMsg(getPeersMsg)
		case event := <-activity.Chan():
			ping.Reset(pingTimeout)
			lastActive = event.(time.Time)
		case t := <-ping.C:
			if lastActive.Add(pingTimeout * 2).Before(t) {
				err = newPeerError(errPingTimeout, "")
			} else if lastActive.Add(pingTimeout).Before(t) {
				err = bp.rw.EncodeMsg(pingMsg)
			}
		}
	}
	return err
}

func (bp *baseProtocol) handle(rw MsgReadWriter) error {
	msg, err := rw.ReadMsg()
	if err != nil {
		return err
	}
	if msg.Size > baseProtocolMaxMsgSize {
		return newPeerError(errMisc, "message too big")
	}
	// make sure that the payload has been fully consumed
	defer msg.Discard()

	switch msg.Code {
	case handshakeMsg:
		return newPeerError(errProtocolBreach, "extra handshake received")

	case discMsg:
		var reason [1]DiscReason
		if err := msg.Decode(&reason); err != nil {
			return err
		}
		return discRequestedError(reason[0])

	case pingMsg:
		return bp.rw.EncodeMsg(pongMsg)

	case pongMsg:

	case getPeersMsg:
		var target [][]byte
		if err := msg.Decode(&target); err != nil {
			return newPeerError(errInvalidMsg, "%v", err)
		}

		peers := bp.peer.getPeers(target...)
		if len(target) == 0 {
			// then add ourselves to the list
			ourAddr := bp.peer.ourListenAddr
			if ourAddr != nil && !ourAddr.IP.IsLoopback() && !ourAddr.IP.IsUnspecified() {
				peers = append(peers, ourAddr)
			}
		}
		ds := make([]interface{}, 0, len(peers))
		// encode and filter out requesting peer
		for _, addr := range peers {
			if addr != bp.peer.listenAddr {
				ds = append(ds, addr)
			}
		}

		if len(ds) > 0 {
			return bp.rw.EncodeMsg(peersMsg, ds...)
		}

	case peersMsg:
		var peers []*peerAddr
		if err := msg.Decode(&peers); err != nil {
			return err
		}
		for _, addr := range peers {
			bp.peer.Debugf("received peer suggestion: %v", addr)
			bp.peer.addPeer(addr)
		}

	default:
		return newPeerError(errInvalidMsgCode, "unknown message code %v", msg.Code)
	}
	return nil
}

func (bp *baseProtocol) readHandshake() error {
	// read and handle remote handshake
	msg, err := bp.rw.ReadMsg()
	if err != nil {
		return err
	}
	if msg.Code != handshakeMsg {
		return newPeerError(errProtocolBreach, "first message must be handshake, got %x", msg.Code)
	}
	if msg.Size > baseProtocolMaxMsgSize {
		return newPeerError(errMisc, "message too big")
	}
	var hs handshake
	if err := msg.Decode(&hs); err != nil {
		return err
	}
	// validate handshake info
	if hs.Version != baseProtocolVersion {
		return newPeerError(errP2PVersionMismatch, "Require protocol %d, received %d\n",
			baseProtocolVersion, hs.Version)
	}
	if len(hs.NodeID) == 0 {
		return newPeerError(errPubkeyMissing, "")
	}
	if len(hs.NodeID) != 64 {
		return newPeerError(errPubkeyInvalid, "require 512 bit, got %v", len(hs.NodeID)*8)
	}
	if da := bp.peer.dialAddr; da != nil {
		// verify that the peer we wanted to connect to
		// actually holds the target public key.
		if da.Pubkey != nil && !bytes.Equal(da.Pubkey, hs.NodeID) {
			return newPeerError(errPubkeyForbidden, "dial address pubkey mismatch")
		}
	}
	pa := newPeerAddr(bp.peer.conn.RemoteAddr(), hs.NodeID)
	if err := bp.peer.pubkeyHook(pa); err != nil {
		return newPeerError(errPubkeyForbidden, "%v", err)
	}
	// TODO: remove Caps with empty name
	var addr *peerAddr
	if hs.ListenPort != 0 {
		addr = newPeerAddr(bp.peer.conn.RemoteAddr(), hs.NodeID)
		addr.Port = hs.ListenPort
	}
	bp.peer.setHandshakeInfo(&hs, addr, hs.Caps)
	bp.peer.startSubprotocols(hs.Caps)
	return nil
}

func (bp *baseProtocol) handshakeMsg() Msg {
	var (
		port uint64
		caps []interface{}
	)
	if bp.peer.ourListenAddr != nil {
		port = bp.peer.ourListenAddr.Port
	}
	for _, proto := range bp.peer.protocols {
		caps = append(caps, proto.cap())
	}
	return NewMsg(handshakeMsg,
		baseProtocolVersion,
		bp.peer.ourID.String(),
		caps,
		port,
		bp.peer.ourID.Pubkey()[1:],
	)
}
