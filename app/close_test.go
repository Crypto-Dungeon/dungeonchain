package app

import (
	"errors"
	"sync"
	"testing"

	"cosmossdk.io/log/v2"
	dbm "github.com/cosmos/cosmos-db"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	"github.com/stretchr/testify/require"
)

type closeCountingDB struct {
	dbm.DB
	count int
	err   error
}

func (db *closeCountingDB) Close() error {
	db.count++
	if db.count > 1 {
		panic("database closed twice")
	}
	if err := db.DB.Close(); err != nil {
		return err
	}
	return db.err
}

func TestCloseIsIdempotent(t *testing.T) {
	for _, closeError := range []error{nil, errors.New("close fixture error")} {
		db := &closeCountingDB{DB: dbm.NewMemDB(), err: closeError}
		app := NewChainApp(log.NewNopLogger(), db, nil, true,
			simtestutil.NewAppOptionsWithFlagHome(t.TempDir()), nil)
		var wg sync.WaitGroup
		results := make(chan error, 16)
		for range 16 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				results <- app.Close()
			}()
		}
		wg.Wait()
		close(results)
		for err := range results {
			require.ErrorIs(t, err, closeError)
		}
		require.Equal(t, 1, db.count)
		require.ErrorIs(t, app.Close(), closeError)
	}
}
