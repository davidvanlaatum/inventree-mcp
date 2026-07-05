//go:build !no_integration_tests

package testenv

import (
	"context"
	"net/netip"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStartInvenTreeStack(t *testing.T) {
	r := require.New(t)
	ctx := context.Background()

	if SkipDocker(os.Getenv) || testing.Short() {
		t.Skipf("Docker-backed InvenTree integration test excluded by %s or -short", EnvSkipDocker)
	}
	t.Parallel()

	opts := DefaultTestOptions(t)
	t.Logf("starting InvenTree integration stack with image %s, expected version %s, expected API %s", opts.Image, opts.ExpectedVersion, opts.ExpectedAPIVersion)
	env, cleanup, err := Start(ctx, opts)
	r.NoError(err)
	r.NotNil(env)
	t.Cleanup(CleanupForTest(t, cleanup))

	r.Equal(DefaultInvenTreeImage, env.Image)
	r.Equal(DefaultVersion, env.Version)
	r.Equal(DefaultAPIVersion, env.APIVersion)
	r.NotEmpty(env.BaseURL)
	r.NotEmpty(env.Token)
	assertPublishedPortsLoopback(t, env)
}

func TestSharedInvenTreeFixturesAndParallelRuns(t *testing.T) {
	r := require.New(t)
	ctx := context.Background()

	if SkipDocker(os.Getenv) || testing.Short() {
		t.Skipf("Docker-backed InvenTree integration test excluded by %s or -short", EnvSkipDocker)
	}
	t.Parallel()

	opts := DefaultTestOptions(t)
	t.Logf("starting shared InvenTree integration stack with image %s, expected version %s, expected API %s", opts.Image, opts.ExpectedVersion, opts.ExpectedAPIVersion)
	shared, err := StartSharedInvenTree(ctx, opts)
	r.NoError(err)
	r.NotNil(shared)
	t.Cleanup(CleanupForTest(t, func() error {
		return shared.Close(context.Background())
	}))
	env := shared.Environment()
	r.NotNil(env)
	prefixes := make(chan string, 2)
	usernames := make(chan string, 2)
	for _, name := range []string{"alpha", "beta"} {
		name := name
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			r := require.New(t)

			run, err := shared.NewRun(t)
			r.NoError(err)
			t.Logf("creating or retrieving InvenTree test account role=%s run_prefix=%s", AccountAdmin, run.Prefix)
			account, err := shared.Account(context.Background(), run, AccountAdmin)
			r.NoError(err)
			t.Logf("using InvenTree test account username=%s role=%s run_prefix=%s", account.Username, account.Role, run.Prefix)
			r.NoError(run.RequireOwnedName(account.Username))
			client, err := shared.Client(account)
			r.NoError(err)
			r.NotNil(client)
			switch name {
			case "alpha":
				category, err := shared.EnsureFixture(context.Background(), account, run, FixtureCategory)
				r.NoError(err)
				r.NoError(run.RequireOwnedName(category.Name))
				r.NotZero(category.ID)
			case "beta":
				supplierPart, err := shared.EnsureFixture(context.Background(), account, run, FixtureSupplierPart)
				r.NoError(err)
				r.NoError(run.RequireOwnedName(supplierPart.Name))
				r.NotZero(supplierPart.ID)
				bom, err := shared.EnsureFixture(context.Background(), account, run, FixtureBOM)
				r.NoError(err)
				r.NoError(run.RequireOwnedName(bom.Name))
				r.NotZero(bom.ID)
			}
			partName, err := run.Name("mutable part")
			r.NoError(err)
			r.NoError(ValidateMutableRecords(run, []MutableRecord{{Name: partName}}))
			r.Error(ValidateMutableRecords(run, []MutableRecord{{Name: "unprefixed"}}))
			r.Error(ValidateMutableRecords(run, []MutableRecord{{Name: "IT_OTHER_TESTENV_TEST_MUTABLE"}}))

			record, err := env.CreateMutableCompany(context.Background(), account, run, "mutable company")
			r.NoError(err)
			r.NoError(run.RequireOwnedName(record.Name))
			r.NotZero(record.ID)

			prefixes <- run.Prefix
			usernames <- account.Username
		})
	}
	t.Cleanup(func() {
		close(prefixes)
		close(usernames)
		seen := map[string]bool{}
		for prefix := range prefixes {
			r.False(seen[prefix], "parallel subtests must receive distinct run prefixes")
			seen[prefix] = true
		}
		r.Len(seen, 2)
		seen = map[string]bool{}
		for username := range usernames {
			r.False(seen[username], "parallel subtests must receive distinct InvenTree accounts")
			seen[username] = true
		}
		r.Len(seen, 2)
	})
}

func assertPublishedPortsLoopback(t *testing.T, env *Environment) {
	t.Helper()
	r := require.New(t)
	ctx := context.Background()

	for _, ctr := range env.containers {
		inspect, err := ctr.Inspect(ctx)
		r.NoError(err)
		for port, bindings := range inspect.NetworkSettings.Ports {
			if len(bindings) == 0 {
				continue
			}
			for _, binding := range bindings {
				r.Equal(netip.MustParseAddr("127.0.0.1"), binding.HostIP, "container %s published %s on a non-loopback address", inspect.Name, port)
			}
		}
	}
}
