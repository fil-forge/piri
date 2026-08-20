// Package base provides testify/suite scaffolding for end-to-end
// integration tests of piri's UCAN HTTP surface (both the body-CAR RPC
// server and the header-container retrieval server).
//
// Embed [BaseSuite] from a concrete suite in a sibling package (e.g.
// .../ucanfxtest/retrieval or .../ucanfxtest/rpc) and add Test* methods.
// The suite stands up the full piri fx app once via SetupSuite and tears
// it down via TearDownSuite, so per-test methods can focus on building
// invocations and asserting on receipts/streamed bytes.
//
// Tests built on this base are slow (they construct the whole fx graph
// and bind a real localhost listener) and should be run with
// -tags=integration.
package base

import (
	"context"
	"net"
	"net/url"
	"time"

	"github.com/fil-forge/ucantone/multikey"
	"github.com/fil-forge/ucantone/testutil"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"go.uber.org/fx"
	"go.uber.org/fx/fxtest"

	"github.com/fil-forge/piri/pkg/fx/app"
	piritestutil "github.com/fil-forge/piri/pkg/internal/testutil"
	"github.com/fil-forge/piri/pkg/internal/testutil/pdpfake"
)

// BaseSuite stands up the full piri fx app once per suite and exposes
// the running HTTP server + service identity. Embed it from concrete
// suites and override SetupTest if you need per-test state reset.
type BaseSuite struct {
	suite.Suite

	App        *fxtest.App
	Echo       *echo.Echo
	ServiceURL *url.URL
	ServiceID  multikey.Issuer

	// ExtraOptions are appended to the fx options before the app is
	// constructed. Subclasses populate this in their own SetupSuite
	// (calling BaseSuite.SetupSuite afterwards) when they need to
	// fx.Decorate dependencies — e.g. swap in a map-based
	// validator.PrincipalResolver to avoid network calls — or to
	// fx.Populate additional fields for use in test methods.
	ExtraOptions []fx.Option

	// ConfigOptions are appended after the default WithSigner option
	// when constructing the test AppConfig. Subclasses use this to
	// install per-suite test fixtures via piritestutil.With* helpers.
	ConfigOptions []piritestutil.TestConfigOption
}

func (s *BaseSuite) SetupSuite() {
	if s.ServiceID == nil {
		s.ServiceID = testutil.RandomMultikeyIssuer(s.T())
	}

	cfgOpts := append(
		[]piritestutil.TestConfigOption{piritestutil.WithIssuer(s.ServiceID)},
		s.ConfigOptions...,
	)
	cfg := piritestutil.NewTestConfig(s.T(), cfgOpts...)
	s.ServiceURL = &cfg.Server.PublicURL

	opts := []fx.Option{
		fx.NopLogger,
		app.CommonModules(cfg),
		app.UCANModule,
		pdpfake.Module,
		fx.Populate(&s.Echo),
	}
	opts = append(opts, s.ExtraOptions...)

	s.App = fxtest.New(s.T(), opts...).RequireStart()
	s.waitReady()
}

func (s *BaseSuite) TearDownSuite() {
	if s.App != nil {
		s.App.RequireStop()
	}
}

// SetupTest runs before each test method. Override in subclasses to
// reset per-test state (clear stores, mint a fresh space DID, etc.).
// Subclass overrides should call BaseSuite.SetupTest() first.
func (s *BaseSuite) SetupTest() {}

// Ctx returns a context bound to the current subtest, so cancellation
// propagates if the test fails or times out.
func (s *BaseSuite) Ctx() context.Context {
	return s.T().Context()
}

// ResolveURL joins a path onto ServiceURL. Useful in helpers that
// target a specific echo route (e.g. /piece/:cid). Exported so suites
// in sibling packages can construct route-specific URLs.
func (s *BaseSuite) ResolveURL(path string) *url.URL {
	u := *s.ServiceURL
	u.Path = path
	return &u
}

// waitReady polls the configured listener until it accepts a TCP
// connection. echofx.StartEchoServer spawns the listener in a goroutine
// from its OnStart hook, so fxtest.RequireStart can return before the
// socket is actually bound.
func (s *BaseSuite) waitReady() {
	deadline := time.Now().Add(5 * time.Second)
	addr := s.ServiceURL.Host
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
		if err == nil {
			conn.Close()
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	require.Failf(s.T(), "server never became ready", "addr=%s", addr)
}
