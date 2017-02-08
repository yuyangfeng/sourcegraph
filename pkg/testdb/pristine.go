package testdb

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"os/exec"

	"sourcegraph.com/sourcegraph/sourcegraph/pkg/dbutil2"
)

var (
	dropSchema   = flag.Bool("db.drop", true, "drop DB tables, then create them, before running tests (required if the DB schema changed)")
	createSchema = flag.Bool("db.create", true, "attempt to create DB tables before running tests")
	truncate     = flag.Bool("db.truncate", true, "truncate (remove all data from) tables before running tests")
	verbose      = flag.Bool("db.v", false, "log DB schema operations in testdb package")

	// initialPoolSize is the number of databases that are created and
	// prepared to be supplied to tests that call
	// PristineDBs. Increasing initialPoolSize makes the initialization time
	// slower but reduces the average wait time for a DB to be
	// supplied to a test.
	initialPoolSize = flag.Int("db.pool", 8, "DB pool size")

	// label is a string that uniquely identifies a package's
	// tests. It is used to create the names of pristine DBs so that
	// they do not conflict with pristine DBs created for other test
	// package processes. Usually this can just be the package name
	// (e.g., "svc" or "sgx"), and it is set automatically from the
	// command-line args (the Go compiled test program is "PKG.test",
	// such as "svc.test").
	label = strings.TrimSuffix(filepath.Base(os.Args[0]), ".test")

	// backgroundDBPoolsByName holds backgroundDBPool objects, each of which
	// maintains a pool of DBs that use a single schema.
	backgroundDBPoolsByName = make(map[string]*backgroundDBPool)

	// backgroundDBPoolsLock protects access to backgroundDBPoolsByName.
	backgroundDBPoolsLock sync.Mutex
)

// pristineDBs returns DB handles to a main DB. The DBs have no data
// in them but the schema (tables/etc.) has been created. The
// underlying DB connection is determined by the env config in the
// same way as for non-test code.
//
// If a background db pool with the given poolName does not exist, a
// new pool will be created using the given schema. Each pool is
// tied to a particular schema. Subsequent calls to pristineDBs with
// an existing poolName must pass in a second argument which is nil or
// is the same schema used to initialize the pool.
//
// NOTE ABOUT DATA LOSS: If you run this func and your env vars are
// set up to access an existing database, its data will be deleted.
func pristineDBs(poolName string, schema *dbutil2.Schema) (main *dbutil2.Handle) {
	var b *backgroundDBPool
	backgroundDBPoolsLock.Lock()
	if _, ok := backgroundDBPoolsByName[poolName]; !ok {
		backgroundDBPoolsByName[poolName] = &backgroundDBPool{}
		backgroundDBPoolsByName[poolName].start(poolName, schema)
	}
	b = backgroundDBPoolsByName[poolName]
	backgroundDBPoolsLock.Unlock()

	if b == nil {
		log.Fatalf("db pool not available: %q", poolName)
	}

	if schema != nil && b.schema != schema {
		log.Fatalf("schema mismatch for db pool: %q", poolName)
	}

	go b.prepareNewDB(poolName)
	const timeout = 45 * time.Second

	select {
	case dbh := <-b.readyDBs:
		if *verbose {
			b.vlog.Printf("got new dbs: %s", dbh.DataSource)
		}
		return dbh
	case <-time.After(timeout):
		log.Fatalf("testdb: DB creation wait exceeded timeout (%s)", timeout)
	}
	panic("unreachable")
}

// backgroundDBPool creates DBs and schemas in the background so
// that there is always a pool of DBs ready to be used by the
// tests. Each backgroundDBPool object maintains a pool of dbs pertaining
// to a single schema.
type backgroundDBPool struct {
	schema *dbutil2.Schema // only 1 schema may be used per db pool

	readyDBs chan *dbutil2.Handle

	// Only drop or create each table once.
	mu      sync.Mutex
	dropped map[int]bool
	created map[int]bool
	nextID  int32

	vlog *log.Logger
}

func (b *backgroundDBPool) start(poolName string, schema *dbutil2.Schema) {
	b.schema = schema

	if label == "" {
		log.Fatal("No label set in package testdb. See the doc comment on label.")
	}

	if *verbose {
		b.vlog = log.New(os.Stderr, "testdb: ", log.Lmicroseconds)
	} else {
		b.vlog = log.New(ioutil.Discard, "", 0)
	}

	dbutil2.CreateUnloggedTables = true

	b.created = map[int]bool{}
	b.dropped = map[int]bool{}
	b.readyDBs = make(chan *dbutil2.Handle, *initialPoolSize)

	for i := 0; i < *initialPoolSize; i++ {
		go b.prepareNewDB(poolName)
	}
}

func (b *backgroundDBPool) prepareNewDB(poolName string) {
	id := atomic.AddInt32(&b.nextID, 1)
	dbname := fmt.Sprintf("sgtmp-%s-%s-%d-%d", poolName, label, id, os.Getpid())
	_ = exec.Command("createdb", dbname).Run()
	dbh, err := dbutil2.Open("dbname="+dbname, *b.schema)
	if err != nil {
		log.Fatal("testdb: open DB:", err)
	}
	b.prepareDBs(int(id), dbh, *dropSchema, *createSchema, *truncate)
	if *verbose {
		b.vlog.Printf("opened new DB (%s)", dbname)
	}
	b.readyDBs <- dbh
}

func (b *backgroundDBPool) prepareDBs(id int, mdb *dbutil2.Handle, drop, create, truncate bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	// Combine all DB handles so we can create schemas concurrently
	// (which is faster).
	if drop && !b.dropped[id] {
		if *verbose {
			b.vlog.Printf("(%d) Dropping schema...", id)
		}
		if err := mdb.DropSchema(); err != nil {
			log.Fatal("testdb: drop schemas:", err)
		}
		b.dropped[id] = true
	}
	if create && !b.created[id] {
		if *verbose {
			b.vlog.Printf("(%d) Creating schema...", id)
		}
		if err := mdb.CreateSchema(); err != nil {
			log.Fatal("testdb: create schemas:", err)
		}
		b.created[id] = true
	}
	if truncate {
		if *verbose {
			b.vlog.Printf("(%d) Truncating all tables...", id)
		}
		if err := mdb.TruncateTables(); err != nil {
			log.Fatal("testdb: truncate all tables:", err)
		}
	}

	if *verbose {
		b.vlog.Printf("(%d) Pristine DBs ready.", id)
	}
}
