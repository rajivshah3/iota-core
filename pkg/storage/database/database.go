package database

import (
	"encoding/json"
	"time"

	"github.com/iotaledger/hive.go/ierrors"
	"github.com/iotaledger/hive.go/kvstore"
	hivedb "github.com/iotaledger/hive.go/kvstore/database"
	"github.com/iotaledger/hive.go/kvstore/mapdb"
	"github.com/iotaledger/hive.go/kvstore/rocksdb"
	"github.com/iotaledger/hive.go/runtime/event"
	"github.com/iotaledger/hive.go/runtime/ioutils"
	"github.com/iotaledger/iota-core/pkg/metrics"
)

var (
	AllowedEnginesDefault = []hivedb.Engine{
		hivedb.EngineAuto,
		hivedb.EngineMapDB,
		hivedb.EngineRocksDB,
	}

	AllowedEnginesStorage = []hivedb.Engine{
		hivedb.EngineRocksDB,
	}

	AllowedEnginesStorageAuto = append(AllowedEnginesStorage, hivedb.EngineAuto)
)

var (
	// ErrNothingToCleanUp is returned when nothing is there to clean up in the database.
	ErrNothingToCleanUp = ierrors.New("Nothing to clean up in the databases")
)

type Cleanup struct {
	Start time.Time
	End   time.Time
}

func (c *Cleanup) MarshalJSON() ([]byte, error) {
	cleanup := struct {
		Start int64 `json:"start"`
		End   int64 `json:"end"`
	}{
		Start: 0,
		End:   0,
	}

	if !c.Start.IsZero() {
		cleanup.Start = c.Start.Unix()
	}

	if !c.End.IsZero() {
		cleanup.End = c.End.Unix()
	}

	return json.Marshal(cleanup)
}

type Events struct {
	Cleanup    *event.Event1[*Cleanup]
	Compaction *event.Event1[bool]
}

// Database holds the underlying KVStore and database specific functions.
type Database struct {
	databaseDir           string
	store                 kvstore.KVStore
	engine                hivedb.Engine
	metrics               *metrics.DatabaseMetrics
	events                *Events
	compactionSupported   bool
	compactionRunningFunc func() bool
}

// New creates a new Database instance.
func New(databaseDirectory string, kvStore kvstore.KVStore, engine hivedb.Engine, metrics *metrics.DatabaseMetrics, events *Events, compactionSupported bool, compactionRunningFunc func() bool) *Database {
	return &Database{
		databaseDir:           databaseDirectory,
		store:                 kvStore,
		engine:                engine,
		metrics:               metrics,
		events:                events,
		compactionSupported:   compactionSupported,
		compactionRunningFunc: compactionRunningFunc,
	}
}

// KVStore returns the underlying KVStore.
func (db *Database) KVStore() kvstore.KVStore {
	return db.store
}

// Engine returns the database engine.
func (db *Database) Engine() hivedb.Engine {
	return db.engine
}

// Metrics returns the database metrics.
func (db *Database) Metrics() *metrics.DatabaseMetrics {
	return db.metrics
}

// Events returns the events of the database.
func (db *Database) Events() *Events {
	return db.events
}

// CompactionSupported returns whether the database engine supports compaction.
func (db *Database) CompactionSupported() bool {
	return db.compactionSupported
}

// CompactionRunning returns whether a compaction is running.
func (db *Database) CompactionRunning() bool {
	if db.compactionRunningFunc == nil {
		return false
	}

	return db.compactionRunningFunc()
}

// Size returns the size of the database.
func (db *Database) Size() (int64, error) {
	if db.engine == hivedb.EngineMapDB {
		// in-memory database does not support this method.
		return 0, nil
	}

	return ioutils.FolderSize(db.databaseDir)
}

// CheckEngine is a wrapper around hivedb.CheckEngine to throw a custom error message in case of engine mismatch.
func CheckEngine(dbPath string, createDatabaseIfNotExists bool, dbEngine hivedb.Engine, allowedEngines ...hivedb.Engine) (hivedb.Engine, error) {

	tmpAllowedEngines := AllowedEnginesDefault
	if len(allowedEngines) > 0 {
		tmpAllowedEngines = allowedEngines
	}

	targetEngine, err := hivedb.CheckEngine(dbPath, createDatabaseIfNotExists, dbEngine, tmpAllowedEngines)
	if err != nil {
		if ierrors.Is(err, hivedb.ErrEngineMismatch) {
			//nolint:stylecheck,revive // this error message is shown to the user
			return hivedb.EngineUnknown, ierrors.Errorf(`database (%s) engine does not match the configuration: '%v' != '%v'

			If you want to use another database engine, you can use the tool './iota-core tool db-migration' to convert the current database.`, dbPath, targetEngine, dbEngine)
		}

		return hivedb.EngineUnknown, err
	}

	return targetEngine, nil
}

// StoreWithDefaultSettings returns a kvstore with default settings.
// It also checks if the database engine is correct.
func StoreWithDefaultSettings(path string, createDatabaseIfNotExists bool, dbEngine hivedb.Engine, allowedEngines ...hivedb.Engine) (kvstore.KVStore, error) {

	tmpAllowedEngines := AllowedEnginesDefault
	if len(allowedEngines) > 0 {
		tmpAllowedEngines = allowedEngines
	}

	targetEngine, err := CheckEngine(path, createDatabaseIfNotExists, dbEngine, tmpAllowedEngines...)
	if err != nil {
		return nil, err
	}

	switch targetEngine {
	case hivedb.EngineRocksDB:
		db, err := NewRocksDB(path)
		if err != nil {
			return nil, err
		}

		return rocksdb.New(db), nil

	case hivedb.EngineMapDB:
		return mapdb.NewMapDB(), nil

	default:
		return nil, ierrors.Errorf("unknown database engine: %s, supported engines: pebble/rocksdb/mapdb", dbEngine)
	}
}
