package cdn

import (
	"bytes"
	"context"
	"crypto/rsa"
	"net/http"
	"sync"
	"testing"

	"github.com/ovh/cds/engine/cdn/objectstore"
	"github.com/ovh/cds/sdk"

	"github.com/ovh/cds/sdk/cdsclient"

	"github.com/stretchr/testify/require"
	"gopkg.in/spacemonkeygo/httpsig.v0"

	"github.com/gorilla/mux"

	"github.com/ovh/cds/engine/api"
	"github.com/ovh/cds/engine/api/authentication"
	"github.com/ovh/cds/engine/api/cache"
	"github.com/ovh/cds/engine/api/test"
	"github.com/ovh/cds/sdk/log"
)

var (
	RedisHost     string
	RedisPassword string
)

const defaultBaseDir = "/tmp/cds"

func init() {
	log.Initialize(&log.Conf{Level: "debug"})
}

func newTestService(t *testing.T) (*Service, error) {
	fakeAPIPrivateKey.Lock()
	defer fakeAPIPrivateKey.Unlock()
	//Read the test config file
	if RedisHost == "" {
		cfg := test.LoadTestingConf(t)
		RedisHost = cfg["redisHost"]
		RedisPassword = cfg["redisPassword"]
	}
	log.SetLogger(t)

	//Prepare the configuration
	cfg := Configuration{}
	cfg.Cache.TTL = 30
	cfg.Cache.Redis.Host = RedisHost
	cfg.Cache.Redis.Password = RedisPassword

	ctx := context.Background()
	r := &api.Router{
		Mux:        mux.NewRouter(),
		Prefix:     "/" + test.GetTestName(t),
		Background: ctx,
	}

	service := new(Service)

	if fakeAPIPrivateKey.key == nil {
		if err := authentication.Init("cds.test", test.TestKey); err != nil {
			t.Error("cannot parse private key")
			return nil, err
		}
		fakeAPIPrivateKey.key = authentication.GetSigningKey()
	}
	service.ParsedAPIPublicKey = &fakeAPIPrivateKey.key.PublicKey
	service.Router = r
	service.initRouter(ctx)
	service.Cfg = cfg

	service.DefaultDriver, _ = objectstore.NewFilesystemStore(sdk.ProjectIntegration{Name: sdk.DefaultStorageIntegrationName}, objectstore.ConfigOptionsFilesystem{BaseDirectory: defaultBaseDir})

	//Init the cache
	var errCache error
	service.Cache, errCache = cache.New(service.Cfg.Cache.Redis.Host, service.Cfg.Cache.Redis.Password, service.Cfg.Cache.TTL)
	if errCache != nil {
		log.Error("Unable to init cache (%s): %v", service.Cfg.Cache.Redis.Host, errCache)
		return nil, errCache
	}

	return service, nil
}

func newRequest(t *testing.T, s *Service, method, uri string, btes []byte, opts ...cdsclient.RequestModifier) *http.Request {
	fakeAPIPrivateKey.Lock()
	defer fakeAPIPrivateKey.Unlock()

	t.Logf("Request: %s %s", method, uri)

	req, err := http.NewRequest(method, uri, bytes.NewBuffer(btes))
	if err != nil {
		t.FailNow()
	}

	for _, opt := range opts {
		opt(req)
	}

	HTTPSigner := httpsig.NewRSASHA256Signer("test", fakeAPIPrivateKey.key, []string{"(request-target)", "host", "date"})
	require.NoError(t, HTTPSigner.Sign(req))

	return req
}

var fakeAPIPrivateKey = struct {
	sync.Mutex
	key *rsa.PrivateKey
}{}