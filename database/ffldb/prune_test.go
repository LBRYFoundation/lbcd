// This file is part of the ffldb package rather than the ffldb_test package
// so it can shrink the block file size to force many files.

package ffldb

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/lbryio/lbcd/chaincfg/chainhash"
	"github.com/lbryio/lbcd/database"
)

// TestPruneBlocks stores the 256 test blocks across many small block files,
// prunes to a target size, and checks what's left, the keepFrom limit, that
// a pruned database reopens, and that the size target can't be below one file.
func TestPruneBlocks(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "ffldb-prune")
	idb, err := database.Create(dbType, dbPath, blockDataNet)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	const fileSize = 2048
	idb.(*db).store.maxBlockFileSize = fileSize

	blocks, err := loadBlocks(t, blockDataFile, blockDataNet)
	if err != nil {
		t.Fatalf("loadBlocks: %v", err)
	}
	for _, b := range blocks {
		if err := idb.Update(func(tx database.Tx) error { return tx.StoreBlock(b) }); err != nil {
			t.Fatalf("store: %v", err)
		}
	}
	first, last, _, err := scanBlockFiles(dbPath)
	if err != nil || first != 0 || last < 10 {
		t.Fatalf("expected many block files, got %d..%d (%v)", first, last, err)
	}

	// The target must hold at least one whole file.
	err = idb.Update(func(tx database.Tx) error {
		_, err := tx.PruneBlocks(fileSize-1, nil)
		return err
	})
	if err == nil {
		t.Fatal("expected an error for a target below one block file")
	}

	// keepFrom stops pruning at the file holding that block.
	keep := blocks[100].Hash()
	var keepFile uint32
	_ = idb.View(func(tx database.Tx) error {
		keepFile = deserializeBlockLoc(tx.(*transaction).blockIdxBucket.Get(keep[:])).blockFileNum
		return nil
	})
	var deleted []chainhash.Hash
	err = idb.Update(func(tx database.Tx) error {
		var err error
		deleted, err = tx.PruneBlocks(fileSize, keep)
		return err
	})
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if len(deleted) == 0 {
		t.Fatal("expected some blocks pruned")
	}
	first, _, _, _ = scanBlockFiles(dbPath)
	if uint32(first) != keepFile {
		t.Fatalf("first remaining file %d, want %d (the one holding block 100)", first, keepFile)
	}
	_ = idb.View(func(tx database.Tx) error {
		if ok, _ := tx.HasBlock(blocks[0].Hash()); ok {
			t.Error("block 0 should be pruned")
		}
		if ok, _ := tx.HasBlock(keep); !ok {
			t.Error("block 100 should be kept")
		}
		if _, err := tx.FetchBlock(blocks[0].Hash()); err == nil {
			t.Error("fetching a pruned block should fail")
		}
		if pruned, _ := tx.BeenPruned(); !pruned {
			t.Error("BeenPruned should be true")
		}
		return nil
	})

	// Without keepFrom, pruning goes down to the target, never deleting the
	// file being written.
	err = idb.Update(func(tx database.Tx) error {
		_, err := tx.PruneBlocks(2*fileSize, nil)
		return err
	})
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	first, last, _, _ = scanBlockFiles(dbPath)
	if last-first > 2 {
		t.Fatalf("files %d..%d left, want at most 3", first, last)
	}
	_ = idb.View(func(tx database.Tx) error {
		if ok, _ := tx.HasBlock(blocks[len(blocks)-1].Hash()); !ok {
			t.Error("the newest block must be kept")
		}
		return nil
	})

	// A pruned database (block files no longer starting at 0) reopens and
	// keeps writing after its last file.
	if err := idb.Close(); err != nil {
		t.Fatal(err)
	}
	idb, err = database.Open(dbType, dbPath, blockDataNet)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer idb.Close()
	_ = idb.View(func(tx database.Tx) error {
		if ok, _ := tx.HasBlock(blocks[len(blocks)-1].Hash()); !ok {
			t.Error("newest block missing after reopen")
		}
		return nil
	})
	if _, err := os.Stat(blockFilePath(dbPath, 0)); !os.IsNotExist(err) {
		t.Error("file 0 should be gone")
	}
}
