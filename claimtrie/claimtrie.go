package claimtrie

import (
	"bytes"
	"fmt"
	"path/filepath"
	"sort"

	"github.com/pkg/errors"

	"github.com/btcsuite/btcd/claimtrie/block"
	"github.com/btcsuite/btcd/claimtrie/block/blockrepo"
	"github.com/btcsuite/btcd/claimtrie/change"
	"github.com/btcsuite/btcd/claimtrie/config"
	"github.com/btcsuite/btcd/claimtrie/merkletrie"
	"github.com/btcsuite/btcd/claimtrie/merkletrie/merkletrierepo"
	"github.com/btcsuite/btcd/claimtrie/node"
	"github.com/btcsuite/btcd/claimtrie/node/noderepo"
	"github.com/btcsuite/btcd/claimtrie/param"
	"github.com/btcsuite/btcd/claimtrie/temporal"
	"github.com/btcsuite/btcd/claimtrie/temporal/temporalrepo"

	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/wire"
)

// ClaimTrie implements a Merkle Trie supporting linear history of commits.
type ClaimTrie struct {

	// Repository for calculated block hashes.
	blockRepo block.Repo

	// Repository for storing temporal information of nodes at each block height.
	// For example, which nodes (by name) should be refreshed at each block height
	// due to stake expiration or delayed activation.
	temporalRepo temporal.Repo

	// Cache layer of Nodes.
	nodeManager node.Manager

	// Prefix tree (trie) that manages merkle hash of each node.
	merkleTrie merkletrie.MerkleTrie

	// Current block height, which is increased by one when AppendBlock() is called.
	height int32

	// Registrered cleanup functions which are invoked in the Close() in reverse order.
	cleanups []func() error
}

func New(cfg config.Config) (*ClaimTrie, error) {

	var cleanups []func() error

	blockRepo, err := blockrepo.NewPebble(filepath.Join(cfg.DataDir, cfg.BlockRepoPebble.Path))
	if err != nil {
		return nil, errors.Wrap(err, "creating block repo")
	}
	cleanups = append(cleanups, blockRepo.Close)

	temporalRepo, err := temporalrepo.NewPebble(filepath.Join(cfg.DataDir, cfg.TemporalRepoPebble.Path))
	if err != nil {
		return nil, errors.Wrap(err, "creating temporal repo")
	}
	cleanups = append(cleanups, temporalRepo.Close)

	// Initialize repository for changes to nodes.
	// The cleanup is delegated to the Node Manager.
	nodeRepo, err := noderepo.NewPebble(filepath.Join(cfg.DataDir, cfg.NodeRepoPebble.Path))
	if err != nil {
		return nil, errors.Wrap(err, "creating node repo")
	}

	baseManager, err := node.NewBaseManager(nodeRepo)
	if err != nil {
		return nil, errors.Wrap(err, "creating node base manager")
	}
	nodeManager := node.NewNormalizingManager(baseManager)
	cleanups = append(cleanups, nodeManager.Close)

	var trie merkletrie.MerkleTrie
	if cfg.RamTrie {
		trie = merkletrie.NewRamTrie(nodeManager)
	} else {

		// Initialize repository for MerkleTrie. The cleanup is delegated to MerkleTrie.
		trieRepo, err := merkletrierepo.NewPebble(filepath.Join(cfg.DataDir, cfg.MerkleTrieRepoPebble.Path))
		if err != nil {
			return nil, errors.Wrap(err, "creating trie repo")
		}

		persistentTrie := merkletrie.NewPersistentTrie(nodeManager, trieRepo)
		cleanups = append(cleanups, persistentTrie.Close)
		trie = persistentTrie
	}

	// Restore the last height.
	previousHeight, err := blockRepo.Load()
	if err != nil {
		return nil, errors.Wrap(err, "load block tip")
	}

	ct := &ClaimTrie{
		blockRepo:    blockRepo,
		temporalRepo: temporalRepo,

		nodeManager: nodeManager,
		merkleTrie:  trie,

		height: previousHeight,
	}

	ct.cleanups = cleanups

	if previousHeight > 0 {
		hash, err := blockRepo.Get(previousHeight)
		if err != nil {
			ct.Close() // TODO: the cleanups aren't run when we exit with an err above here (but should be)
			return nil, errors.Wrap(err, "block repo get")
		}
		_, err = nodeManager.IncrementHeightTo(previousHeight)
		if err != nil {
			ct.Close()
			return nil, errors.Wrap(err, "increment height to")
		}
		// TODO: pass in the interrupt signal here:
		trie.SetRoot(hash, nil) // keep this after IncrementHeightTo

		if !ct.MerkleHash().IsEqual(hash) {
			ct.Close()
			return nil, errors.Errorf("unable to restore the claim hash to %s at height %d", hash.String(), previousHeight)
		}
	}

	return ct, nil
}

// AddClaim adds a Claim to the ClaimTrie.
func (ct *ClaimTrie) AddClaim(name []byte, op wire.OutPoint, id change.ClaimID, amt int64) error {

	chg := change.Change{
		Type:     change.AddClaim,
		Name:     name,
		OutPoint: op,
		Amount:   amt,
		ClaimID:  id,
	}

	return ct.forwardNodeChange(chg)
}

// UpdateClaim updates a Claim in the ClaimTrie.
func (ct *ClaimTrie) UpdateClaim(name []byte, op wire.OutPoint, amt int64, id change.ClaimID) error {

	chg := change.Change{
		Type:     change.UpdateClaim,
		Name:     name,
		OutPoint: op,
		Amount:   amt,
		ClaimID:  id,
	}

	return ct.forwardNodeChange(chg)
}

// SpendClaim spends a Claim in the ClaimTrie.
func (ct *ClaimTrie) SpendClaim(name []byte, op wire.OutPoint, id change.ClaimID) error {

	chg := change.Change{
		Type:     change.SpendClaim,
		Name:     name,
		OutPoint: op,
		ClaimID:  id,
	}

	return ct.forwardNodeChange(chg)
}

// AddSupport adds a Support to the ClaimTrie.
func (ct *ClaimTrie) AddSupport(name []byte, op wire.OutPoint, amt int64, id change.ClaimID) error {

	chg := change.Change{
		Type:     change.AddSupport,
		Name:     name,
		OutPoint: op,
		Amount:   amt,
		ClaimID:  id,
	}

	return ct.forwardNodeChange(chg)
}

// SpendSupport spends a Support in the ClaimTrie.
func (ct *ClaimTrie) SpendSupport(name []byte, op wire.OutPoint, id change.ClaimID) error {

	chg := change.Change{
		Type:     change.SpendSupport,
		Name:     name,
		OutPoint: op,
		ClaimID:  id,
	}

	return ct.forwardNodeChange(chg)
}

// AppendBlock increases block by one.
func (ct *ClaimTrie) AppendBlock() error {

	ct.height++

	names, err := ct.nodeManager.IncrementHeightTo(ct.height)
	if err != nil {
		return errors.Wrap(err, "node manager increment")
	}

	expirations, err := ct.temporalRepo.NodesAt(ct.height)
	if err != nil {
		return errors.Wrap(err, "temporal repo get")
	}

	names = removeDuplicates(names) // comes out sorted

	updateNames := make([][]byte, 0, len(names)+len(expirations))
	updateHeights := make([]int32, 0, len(names)+len(expirations))
	updateNames = append(updateNames, names...)
	for range names { // log to the db that we updated a name at this height for rollback purposes
		updateHeights = append(updateHeights, ct.height)
	}
	names = append(names, expirations...)
	names = removeDuplicates(names)

	for _, name := range names {

		ct.merkleTrie.Update(name, true)

		newName, nextUpdate := ct.nodeManager.NextUpdateHeightOfNode(name)
		if nextUpdate <= 0 {
			continue // some names are no longer there; that's not an error
		}
		updateNames = append(updateNames, newName) // TODO: make sure using the temporalRepo batch is actually faster
		updateHeights = append(updateHeights, nextUpdate)
	}
	err = ct.temporalRepo.SetNodesAt(updateNames, updateHeights)
	if err != nil {
		return errors.Wrap(err, "temporal repo set")
	}

	hitFork := ct.updateTrieForHashForkIfNecessary()

	h := ct.MerkleHash()
	ct.blockRepo.Set(ct.height, h)

	if hitFork {
		ct.merkleTrie.SetRoot(h, names) // for clearing the memory entirely
	}

	return nil
}

func (ct *ClaimTrie) updateTrieForHashForkIfNecessary() bool {
	if ct.height != param.AllClaimsInMerkleForkHeight {
		return false
	}

	node.LogOnce("Marking all trie nodes as dirty for the hash fork...")

	// invalidate all names because we have to recompute the hash on everything
	ct.nodeManager.IterateNames(func(name []byte) bool {
		ct.merkleTrie.Update(name, false)
		return true
	})

	node.LogOnce("Done. Now recomputing all hashes...")
	return true
}

func removeDuplicates(names [][]byte) [][]byte { // this might be too expensive; we'll have to profile it
	sort.Slice(names, func(i, j int) bool { // put names in order so we can skip duplicates
		return bytes.Compare(names[i], names[j]) < 0
	})

	for i := len(names) - 2; i >= 0; i-- {
		if bytes.Equal(names[i], names[i+1]) {
			names = append(names[:i], names[i+1:]...)
		}
	}
	return names
}

// ResetHeight resets the ClaimTrie to a previous known height..
func (ct *ClaimTrie) ResetHeight(height int32) error {

	names := make([][]byte, 0)
	for h := height + 1; h <= ct.height; h++ {
		results, err := ct.temporalRepo.NodesAt(h)
		if err != nil {
			return err
		}
		names = append(names, results...)
	}
	err := ct.nodeManager.DecrementHeightTo(names, height)
	if err != nil {
		return err
	}

	passedHashFork := ct.height >= param.AllClaimsInMerkleForkHeight && height < param.AllClaimsInMerkleForkHeight
	ct.height = height
	hash, err := ct.blockRepo.Get(height)
	if err != nil {
		return err
	}

	if passedHashFork {
		names = nil // force them to reconsider all names
	}
	ct.merkleTrie.SetRoot(hash, names)

	if !ct.MerkleHash().IsEqual(hash) {
		return errors.Errorf("unable to restore the hash at height %d", height)
	}
	return nil
}

// MerkleHash returns the Merkle Hash of the claimTrie.
func (ct *ClaimTrie) MerkleHash() *chainhash.Hash {
	if ct.height >= param.AllClaimsInMerkleForkHeight {
		return ct.merkleTrie.MerkleHashAllClaims()
	}
	return ct.merkleTrie.MerkleHash()
}

// Height returns the current block height.
func (ct *ClaimTrie) Height() int32 {
	return ct.height
}

// Close persists states.
// Any calls to the ClaimTrie after Close() being called results undefined behaviour.
func (ct *ClaimTrie) Close() {

	for i := len(ct.cleanups) - 1; i >= 0; i-- {
		cleanup := ct.cleanups[i]
		err := cleanup()
		if err != nil { // it would be better to cleanup what we can than exit early
			node.LogOnce("On cleanup: " + err.Error())
		}
	}
}

func (ct *ClaimTrie) forwardNodeChange(chg change.Change) error {

	chg.Height = ct.Height() + 1

	err := ct.nodeManager.AppendChange(chg)
	if err != nil {
		return fmt.Errorf("node manager handle change: %w", err)
	}

	return nil
}

func (ct *ClaimTrie) Node(name []byte) (*node.Node, error) {
	return ct.nodeManager.Node(name)
}

func (ct *ClaimTrie) FlushToDisk() {
	// maybe the user can fix the file lock shown in the warning before they shut down
	if err := ct.nodeManager.Flush(); err != nil {
		node.Warn("During nodeManager flush: " + err.Error())
	}
	if err := ct.temporalRepo.Flush(); err != nil {
		node.Warn("During temporalRepo flush: " + err.Error())
	}
	if err := ct.merkleTrie.Flush(); err != nil {
		node.Warn("During merkleTrie flush: " + err.Error())
	}
	if err := ct.blockRepo.Flush(); err != nil {
		node.Warn("During blockRepo flush: " + err.Error())
	}
}
