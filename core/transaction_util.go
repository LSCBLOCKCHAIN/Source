// Copyright 2015 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package core

import (
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/access"
	"github.com/ethereum/go-ethereum/core/requests"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/logger"
	"github.com/ethereum/go-ethereum/logger/glog"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/syndtr/goleveldb/leveldb"
	"golang.org/x/net/context"
)

var (
	blockReceiptsPre = []byte("receipts-block-")
	receiptsPre      = []byte("receipts-")
)

// PutTransactions stores the transactions in the given database
func PutTransactions(db ethdb.Database, block *types.Block, txs types.Transactions) error {
	batch := db.NewBatch()

	for i, tx := range block.Transactions() {
		rlpEnc, err := rlp.EncodeToBytes(tx)
		if err != nil {
			return fmt.Errorf("failed encoding tx: %v", err)
		}

		batch.Put(tx.Hash().Bytes(), rlpEnc)

		var txExtra struct {
			BlockHash  common.Hash
			BlockIndex uint64
			Index      uint64
		}
		txExtra.BlockHash = block.Hash()
		txExtra.BlockIndex = block.NumberU64()
		txExtra.Index = uint64(i)
		rlpMeta, err := rlp.EncodeToBytes(txExtra)
		if err != nil {
			return fmt.Errorf("failed encoding tx meta data: %v", err)
		}

		batch.Put(append(tx.Hash().Bytes(), 0x0001), rlpMeta)
	}

	if err := batch.Write(); err != nil {
		return fmt.Errorf("failed writing tx to db: %v", err)
	}
	return nil
}

func DeleteTransaction(db ethdb.Database, txHash common.Hash) {
	db.Delete(txHash[:])
}

func GetTransaction(db ethdb.Database, txhash common.Hash) *types.Transaction {
	data, _ := db.Get(txhash[:])
	if len(data) != 0 {
		var tx types.Transaction
		if err := rlp.DecodeBytes(data, &tx); err != nil {
			return nil
		}
		return &tx
	}
	return nil
}

// PutReceipts stores the receipts in the current database
func PutReceipts(db ethdb.Database, receipts types.Receipts) error {
	batch := new(leveldb.Batch)
	_, batchWrite := db.(*ethdb.LDBDatabase)

	for _, receipt := range receipts {
		storageReceipt := (*types.ReceiptForStorage)(receipt)
		bytes, err := rlp.EncodeToBytes(storageReceipt)
		if err != nil {
			return err
		}

		if batchWrite {
			batch.Put(append(receiptsPre, receipt.TxHash[:]...), bytes)
		} else {
			err = db.Put(append(receiptsPre, receipt.TxHash[:]...), bytes)
			if err != nil {
				return err
			}
		}
	}
	if db, ok := db.(*ethdb.LDBDatabase); ok {
		if err := db.LDB().Write(batch, nil); err != nil {
			return err
		}
	}

	return nil
}

// Delete a receipts from the database
func DeleteReceipt(db ethdb.Database, txHash common.Hash) {
	db.Delete(append(receiptsPre, txHash[:]...))
}

// GetReceipt returns a receipt by hash
func GetReceipt(ca *access.ChainAccess, txHash common.Hash) *types.Receipt {
	data, _ := ca.Db().Get(append(receiptsPre, txHash[:]...))
	if len(data) == 0 {
		return nil
	}
	var receipt types.ReceiptForStorage
	err := rlp.DecodeBytes(data, &receipt)
	if err != nil {
		glog.V(logger.Core).Infoln("GetReceipt err:", err)
	}
	return (*types.Receipt)(&receipt)
}

// GetBlockReceipts returns the receipts generated by the transactions
// included in block's given hash.
func GetBlockReceipts(ca *access.ChainAccess, hash common.Hash) types.Receipts {
	return GetBlockReceiptsOdr(access.NoOdr, ca, hash)
}

// GetBlockReceiptsOdr returns the receipts generated by the transactions
// included in block's given hash from the database or network.
func GetBlockReceiptsOdr(ctx context.Context, ca *access.ChainAccess, hash common.Hash) types.Receipts {
	r := requests.NewReceiptsAccess(ca.Db(), hash, GetHeader, PutReceipts, PutBlockReceipts)
	ca.Retrieve(ctx, r)
	return r.GetReceipts()
}

// PutBlockReceipts stores the block's transactions associated receipts
// and stores them by block hash in a single slice. This is required for
// forks and chain reorgs
func PutBlockReceipts(db ethdb.Database, hash common.Hash, receipts types.Receipts) error {
	rs := make([]*types.ReceiptForStorage, len(receipts))
	for i, receipt := range receipts {
		rs[i] = (*types.ReceiptForStorage)(receipt)
	}
	bytes, err := rlp.EncodeToBytes(rs)
	if err != nil {
		return err
	}
	err = db.Put(append(blockReceiptsPre, hash[:]...), bytes)
	if err != nil {
		return err
	}
	return nil
}
