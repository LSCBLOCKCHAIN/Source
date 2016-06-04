package api

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

var (
	hexprvkey     = "65138b2aa745041b372153550584587da326ab440576b2a1191dd95cee30039c"
	defaultConfig = `{
    "ChunkDbPath": "` + filepath.Join("TMPDIR", "0d2f62485607cf38d9d795d93682a517661e513e", "chunks") + `",
    "DbCapacity": 5000000,
    "CacheCapacity": 5000,
    "Radius": 0,
    "Branches": 128,
    "Hash": "SHA256",
    "JoinTimeout": 120,
    "SplitTimeout": 120,
    "CallInterval": 3000000000,
    "KadDbPath": "` + filepath.Join("TMPDIR", "0d2f62485607cf38d9d795d93682a517661e513e", "bzz-peers.json") + `",
    "MaxProx": 8,
    "ProxBinSize": 4,
    "BucketSize": 3,
    "PurgeInterval": 151200000000000,
    "InitialRetryInterval": 4200000000,
    "ConnRetryExp": 2,
    "Swap": {
        "BuyAt": 20000000000,
        "SellAt": 20000000000,
        "PayAt": 100,
        "DropAt": 10000,
        "AutoCashInterval": 300000000000,
        "AutoCashThreshold": 50000000000000,
        "AutoDepositInterval": 300000000000,
        "AutoDepositThreshold": 50000000000000,
        "AutoDepositBuffer": 100000000000000,
        "PublicKey": "0x045f5cfd26692e48d0017d380349bcf50982488bc11b5145f3ddf88b24924299048450542d43527fbe29a5cb32f38d62755393ac002e6bfdd71b8d7ba725ecd7a3",
        "Contract": "0x0000000000000000000000000000000000000000",
        "Beneficiary": "0x0d2f62485607cf38d9d795d93682a517661e513e"
    },
    "RequestDbPath": "` + filepath.Join("TMPDIR", "0d2f62485607cf38d9d795d93682a517661e513e", "requests") + `",
    "RequestDbBatchSize": 512,
    "KeyBufferSize": 1024,
    "SyncBatchSize": 128,
    "SyncBufferSize": 128,
    "SyncCacheSize": 1024,
    "SyncPriorities": [
        2,
        1,
        1,
        0,
        0
    ],
    "SyncModes": [
        true,
        true,
        true,
        true,
        false
    ],
    "Path": "` + filepath.Join("TMPDIR", "0d2f62485607cf38d9d795d93682a517661e513e") + `",
    "Port": "8500",
    "PublicKey": "0x045f5cfd26692e48d0017d380349bcf50982488bc11b5145f3ddf88b24924299048450542d43527fbe29a5cb32f38d62755393ac002e6bfdd71b8d7ba725ecd7a3",
    "BzzKey": "0xe861964402c0b78e2d44098329b8545726f215afa737d803714a4338552fcb81"
}`
)

func TestConfigWriteRead(t *testing.T) {
	tmp, err := ioutil.TempDir(os.TempDir(), "bzz-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmp)

	prvkey := crypto.ToECDSA(common.Hex2Bytes(hexprvkey))
	orig, err := NewConfig(tmp, common.Address{}, prvkey)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	account := crypto.PubkeyToAddress(prvkey.PublicKey)
	dirpath := filepath.Join(tmp, common.Bytes2Hex(account.Bytes()))
	confpath := filepath.Join(dirpath, "config.json")
	data, err := ioutil.ReadFile(confpath)
	if err != nil {
		t.Fatalf("default config file cannot be read: %v", err)
	}
	exp := strings.Replace(defaultConfig, "TMPDIR", tmp, -1)
	exp = strings.Replace(exp, "\\", "\\\\", -1)

	if string(data) != exp {
		t.Fatalf("default config mismatch:\nexpected:\n'%v'\ngot:\n'%v'", exp, string(data))
	}

	conf, err := NewConfig(tmp, common.Address{}, prvkey)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if conf.Swap.Beneficiary.Hex() != orig.Swap.Beneficiary.Hex() {
		t.Fatalf("expected beneficiary from loaded config %v to match original %v", conf.Swap.Beneficiary.Hex(), orig.Swap.Beneficiary.Hex())
	}

}
