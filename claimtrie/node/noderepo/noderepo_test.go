package noderepo

import (
	"testing"

	"github.com/lbryio/lbcd/claimtrie/change"
	"github.com/lbryio/lbcd/claimtrie/node"

	"github.com/stretchr/testify/require"
)

var (
	out1          = node.NewOutPointFromString("0000000000000000000000000000000000000000000000000000000000000000:1")
	testNodeName1 = []byte("name1")
)

func TestPebble(t *testing.T) {

	r := require.New(t)

	repo, err := NewPebble(t.TempDir())
	r.NoError(err)
	defer func() {
		err := repo.Close()
		r.NoError(err)
	}()

	cleanup := func() {
		lowerBound := testNodeName1
		upperBound := append(testNodeName1, byte(0))
		err := repo.db.DeleteRange(lowerBound, upperBound, nil)
		r.NoError(err)
	}

	testNodeRepo(t, repo, func() {}, cleanup)
}

func testNodeRepo(t *testing.T, repo node.Repo, setup, cleanup func()) {

	r := require.New(t)

	chg := change.NewChange(change.AddClaim).SetName(testNodeName1).SetOutPoint(out1)

	testcases := []struct {
		name     string
		height   int32
		changes  []change.Change
		expected []change.Change
	}{
		{
			"test 1",
			1,
			[]change.Change{chg.SetHeight(1), chg.SetHeight(3), chg.SetHeight(5)},
			[]change.Change{chg.SetHeight(1)},
		},
		{
			"test 2",
			2,
			[]change.Change{chg.SetHeight(1), chg.SetHeight(3), chg.SetHeight(5)},
			[]change.Change{chg.SetHeight(1)},
		},
		{
			"test 3",
			3,
			[]change.Change{chg.SetHeight(1), chg.SetHeight(3), chg.SetHeight(5)},
			[]change.Change{chg.SetHeight(1), chg.SetHeight(3)},
		},
		{
			"test 4",
			4,
			[]change.Change{chg.SetHeight(1), chg.SetHeight(3), chg.SetHeight(5)},
			[]change.Change{chg.SetHeight(1), chg.SetHeight(3)},
		},
		{
			"test 5",
			5,
			[]change.Change{chg.SetHeight(1), chg.SetHeight(3), chg.SetHeight(5)},
			[]change.Change{chg.SetHeight(1), chg.SetHeight(3), chg.SetHeight(5)},
		},
		{
			"test 6",
			6,
			[]change.Change{chg.SetHeight(1), chg.SetHeight(3), chg.SetHeight(5)},
			[]change.Change{chg.SetHeight(1), chg.SetHeight(3), chg.SetHeight(5)},
		},
	}

	for _, tt := range testcases {

		setup()

		err := repo.AppendChanges(tt.changes)
		r.NoError(err)

		changes, err := repo.LoadChanges(testNodeName1)
		r.NoError(err)
		r.Equalf(tt.expected, changes[:len(tt.expected)], tt.name)

		cleanup()
	}

	testcases2 := []struct {
		name     string
		height   int32
		changes  [][]change.Change
		expected []change.Change
	}{
		{
			"Save in 2 batches, and load up to 1",
			1,
			[][]change.Change{
				{chg.SetHeight(1), chg.SetHeight(3), chg.SetHeight(5)},
				{chg.SetHeight(6), chg.SetHeight(8), chg.SetHeight(9)},
			},
			[]change.Change{chg.SetHeight(1)},
		},
		{
			"Save in 2 batches, and load up to 9",
			9,
			[][]change.Change{
				{chg.SetHeight(1), chg.SetHeight(3), chg.SetHeight(5)},
				{chg.SetHeight(6), chg.SetHeight(8), chg.SetHeight(9)},
			},
			[]change.Change{
				chg.SetHeight(1), chg.SetHeight(3), chg.SetHeight(5),
				chg.SetHeight(6), chg.SetHeight(8), chg.SetHeight(9),
			},
		},
		{
			"Save in 3 batches, and load up to 8",
			8,
			[][]change.Change{
				{chg.SetHeight(1), chg.SetHeight(3)},
				{chg.SetHeight(5)},
				{chg.SetHeight(6), chg.SetHeight(8), chg.SetHeight(9)},
			},
			[]change.Change{
				chg.SetHeight(1), chg.SetHeight(3), chg.SetHeight(5),
				chg.SetHeight(6), chg.SetHeight(8),
			},
		},
	}

	for _, tt := range testcases2 {

		setup()

		for _, changes := range tt.changes {
			err := repo.AppendChanges(changes)
			r.NoError(err)
		}

		changes, err := repo.LoadChanges(testNodeName1)
		r.NoError(err)
		r.Equalf(tt.expected, changes[:len(tt.expected)], tt.name)

		cleanup()
	}
}

func TestIterator(t *testing.T) {

	r := require.New(t)

	repo, err := NewPebble(t.TempDir())
	r.NoError(err)
	defer func() {
		err := repo.Close()
		r.NoError(err)
	}()

	creation := []change.Change{
		{Name: []byte("test\x00"), Height: 5},
		{Name: []byte("test\x00\x00"), Height: 5},
		{Name: []byte("test\x00b"), Height: 5},
		{Name: []byte("test\x00\xFF"), Height: 5},
		{Name: []byte("testa"), Height: 5},
	}
	err = repo.AppendChanges(creation)
	r.NoError(err)

	i := 0
	repo.IterateChildren([]byte{}, func(changes []change.Change) bool {
		r.Equal(creation[i], changes[0])
		i++
		return true
	})
}

func TestDropChangesAbove(t *testing.T) {

	r := require.New(t)

	repo, err := NewPebble(t.TempDir())
	r.NoError(err)
	defer func() {
		err := repo.Close()
		r.NoError(err)
	}()

	chg := change.NewChange(change.AddClaim).SetOutPoint(out1)
	name1, name2, name3 := []byte("name1"), []byte("name2"), []byte("name3")

	err = repo.AppendChanges([]change.Change{
		chg.SetName(name1).SetHeight(1), chg.SetName(name1).SetHeight(3),
		chg.SetName(name2).SetHeight(2),
		chg.SetName(name3).SetHeight(4), chg.SetName(name3).SetHeight(5),
	})
	r.NoError(err)

	names, err := repo.DropChangesAbove(2)
	r.NoError(err)
	r.Equal([][]byte{name1, name3}, names)

	changes, err := repo.LoadChanges(name1)
	r.NoError(err)
	r.Equal([]change.Change{chg.SetName(name1).SetHeight(1)}, changes)

	changes, err = repo.LoadChanges(name2)
	r.NoError(err)
	r.Equal([]change.Change{chg.SetName(name2).SetHeight(2)}, changes)

	changes, err = repo.LoadChanges(name3)
	r.NoError(err)
	r.Empty(changes)
}
