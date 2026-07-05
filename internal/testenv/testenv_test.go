package testenv

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/moby/moby/api/types/container"
	dockernetwork "github.com/moby/moby/api/types/network"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
)

func TestDefaultOptionsPinInvenTreeVersion(t *testing.T) {
	t.Parallel()
	a := assert.New(t)

	opts := DefaultOptions()

	a.Equal("inventree/inventree:1.4.0", opts.Image)
	a.Equal("1.4.0", opts.ExpectedVersion)
	a.Equal("511", opts.ExpectedAPIVersion)
	a.NoError(ValidateOptions(opts))
}

func TestDefaultTestOptionsForwardsContainerLogs(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	opts := DefaultTestOptions(t)

	r.NotNil(opts.ContainerLogf)
	opts.ContainerLogf("inventree", "stdout", "ready")
}

func TestValidateOptionsRejectsFloatingOrAmbiguousImages(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		image string
	}{
		{name: "missing tag", image: "inventree/inventree"},
		{name: "stable", image: "inventree/inventree:stable"},
		{name: "latest", image: "inventree/inventree:latest"},
		{name: "digest", image: "inventree/inventree@sha256:abc123"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := require.New(t)

			opts := DefaultOptions()
			opts.Image = tt.image
			err := ValidateOptions(opts)

			r.Error(err)
		})
	}
}

func TestValidateOptionsRejectsMissingVersionAPIAndNegativeTimeout(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	opts := DefaultOptions()
	opts.ExpectedVersion = ""
	opts.ExpectedAPIVersion = ""
	opts.StartupTimeout = -1

	err := ValidateOptions(opts)

	r.Error(err)
	r.ErrorContains(err, "expected InvenTree version is required")
	r.ErrorContains(err, "expected InvenTree API version is required")
	r.ErrorContains(err, "startup timeout must not be negative")
}

func TestSkipDockerParsesExplicitExclusion(t *testing.T) {
	t.Parallel()
	a := assert.New(t)

	a.True(SkipDocker(func(string) string { return "1" }))
	a.True(SkipDocker(func(string) string { return "true" }))
	a.True(SkipDocker(func(string) string { return "yes" }))
	a.False(SkipDocker(func(string) string { return "" }))
	a.False(SkipDocker(func(string) string { return "0" }))
	a.False(SkipDocker(nil))
}

func TestRunPrefixAndMutableRecordOwnership(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	shared := &SharedInvenTree{runID: "ABC123"}

	run, err := shared.NewRun(t)
	r.NoError(err)
	r.Equal("ABC123", run.ID)
	r.Equal("TESTENV", run.Package)
	r.Equal("TESTRUNPREFIXANDMUTABLERECORDOWNERSHIP", run.Test)
	r.Equal("IT_ABC123_TESTENV_TESTRUNPREFIXANDMUTABLERECORDOWNERSHIP_", run.Prefix)

	name, err := run.Name("part 1")
	r.NoError(err)
	r.Equal(run.Prefix+"PART1", name)

	a.NoError(ValidateMutableRecords(run, []MutableRecord{{Name: name}}))
	a.Error(ValidateMutableRecords(run, []MutableRecord{{Name: "PART1"}}))
	a.Error(ValidateMutableRecords(run, []MutableRecord{{Name: "IT_OTHER_TESTENV_TESTRUNPREFIXANDMUTABLERECORDOWNERSHIP_PART1"}}))
	a.Error(ValidateMutableRecords(nil, []MutableRecord{{Name: name}}))
}

func TestNewRunRejectsUnsafeSegments(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	_, err := newRun("bad-id", "TESTENV", "TEST")
	r.Error(err)
	_, err = newRun("ABC123", "TEST/ENV", "TEST")
	r.Error(err)
	_, err = newRun("ABC123", "TESTENV", "TEST_NAME")
	r.Error(err)
}

func TestIntegrationDockerSkipEnvironmentName(t *testing.T) {
	a := assert.New(t)

	t.Setenv(EnvSkipDocker, "true")

	a.True(SkipDocker(os.Getenv))
}

func TestStartSharedInvenTreeDoesNotSeedFixtures(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	ctx := context.Background()

	starts := 0
	cleanups := 0
	shared, err := startSharedInvenTree(ctx, Options{
		HTTPClient: fakeHTTPClient(t, func(*http.Request) (int, string) {
			t.Fatal("start shared InvenTree must not create fixtures until a subtest asks for them")
			return http.StatusInternalServerError, `{}`
		}),
	}, func(context.Context, Options) (*Environment, CleanupFunc, error) {
		starts++
		return &Environment{BaseURL: "http://inventree.test", Token: "test-token"}, func() error {
			cleanups++
			return nil
		}, nil
	})

	r.NoError(err)
	r.NotNil(shared)
	r.NotNil(shared.Environment())
	r.Equal(1, starts)
	r.NoError(shared.Close(ctx))
	r.Equal(1, cleanups)
}

func TestSharedInvenTreeWrapperNilAndClientPaths(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	var shared *SharedInvenTree
	r.Nil(shared.Environment())
	r.NoError(shared.Close(context.Background()))
	_, err := shared.Account(context.Background(), &Run{}, AccountAdmin)
	r.Error(err)
	_, err = shared.Client(&Account{})
	r.Error(err)
	_, err = shared.EnsureFixture(context.Background(), &Account{}, &Run{}, FixtureCategory)
	r.Error(err)

	run, err := newRun("ABC123", "TESTENV", "TESTSHAREDWRAPPER")
	r.NoError(err)
	accountName, err := run.Name("user")
	r.NoError(err)
	account := &Account{Username: accountName, Token: "account-token", Role: AccountAdmin, run: run}
	shared = &SharedInvenTree{env: &Environment{BaseURL: "http://inventree.test"}}
	client, err := shared.Client(account)
	r.NoError(err)
	r.NotNil(client)
}

func TestAccountCreatesRunScopedInvenTreeUserAndToken(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	ctx := context.Background()

	run, err := newRun("ABC123", "TESTENV", "TESTACCOUNT")
	r.NoError(err)
	username, err := run.Name("user")
	r.NoError(err)
	userID := 42
	userCreated := false
	client := fakeHTTPClient(t, func(req *http.Request) (int, string) {
		switch req.URL.Path {
		case "/api/user/":
			r.Equal("Token admin-token", req.Header.Get("Authorization"))
			switch req.Method {
			case http.MethodGet:
				r.Equal(username, req.URL.Query().Get("search"))
				r.Equal("10", req.URL.Query().Get("limit"))
				if userCreated {
					return http.StatusOK, fmtJSON(testenvListResponse{Results: []testenvRecord{{PK: userID, Username: username}}})
				}
				return http.StatusOK, `{"results":[]}`
			case http.MethodPost:
				payload := map[string]any{}
				r.NoError(json.NewDecoder(req.Body).Decode(&payload))
				r.Equal(username, payload["username"])
				r.Equal(strings.ToLower(username)+"@example.test", payload["email"])
				r.Equal(true, payload["is_staff"])
				r.Equal(true, payload["is_superuser"])
				userCreated = true
				return http.StatusCreated, fmtJSON(testenvRecord{PK: userID, Username: username})
			default:
				return http.StatusMethodNotAllowed, `{}`
			}
		case fmt.Sprintf("/api/user/%d/set-password/", userID):
			r.Equal("Token admin-token", req.Header.Get("Authorization"))
			r.Equal(http.MethodPut, req.Method)
			payload := map[string]any{}
			r.NoError(json.NewDecoder(req.Body).Decode(&payload))
			r.NotEmpty(payload["password"])
			r.Equal(true, payload["override_warning"])
			return http.StatusOK, `{}`
		case "/api/user/me/token/":
			user, password, ok := req.BasicAuth()
			r.True(ok)
			r.Equal(username, user)
			r.NotEmpty(password)
			r.Equal(defaultTokenName, req.URL.Query().Get("name"))
			return http.StatusOK, `{"token":"account-token"}`
		default:
			return http.StatusNotFound, `{}`
		}
	})
	shared, err := startSharedInvenTree(ctx, Options{HTTPClient: client}, func(context.Context, Options) (*Environment, CleanupFunc, error) {
		return &Environment{BaseURL: "http://inventree.test", Token: "admin-token"}, func() error {
			return nil
		}, nil
	})
	r.NoError(err)

	account, err := shared.Account(ctx, run, AccountAdmin)

	r.NoError(err)
	r.Equal(username, account.Username)
	r.Equal("account-token", account.Token)
	r.Equal(AccountAdmin, account.Role)
	r.NoError(run.RequireOwnedName(account.Username))

	account, err = shared.Account(ctx, run, AccountAdmin)
	r.NoError(err)
	r.Equal(username, account.Username)
	r.Equal("account-token", account.Token)
}

func TestAccountRejectsUnsupportedRoleAndInvalidEnvironment(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	ctx := context.Background()

	run, err := newRun("ABC123", "TESTENV", "TESTACCOUNTREJECTS")
	r.NoError(err)
	env := &Environment{BaseURL: "http://inventree.test", Token: "admin-token"}

	account, err := env.Account(ctx, run, AccountRole("readonly"))
	r.Error(err)
	r.Nil(account)

	account, err = (*Environment)(nil).Account(ctx, run, AccountAdmin)
	r.Error(err)
	r.Nil(account)
}

func TestClientRejectsMissingAccountToken(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	env := &Environment{BaseURL: "http://inventree.test", Token: "admin-token"}
	client, err := env.Client(nil)
	r.Error(err)
	r.Nil(client)

	client, err = env.Client(&Account{Username: "IT_ABC123_TESTENV_TEST_USER"})
	r.Error(err)
	r.Nil(client)
}

func TestCreateMutableCompanyUsesRunScopedAccount(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	ctx := context.Background()

	run, err := newRun("ABC123", "TESTENV", "TESTCREATEMUTABLE")
	r.NoError(err)
	accountName, err := run.Name("user")
	r.NoError(err)
	account := &Account{Username: accountName, Token: "account-token", Role: AccountAdmin, run: run}
	wantName, err := run.Name("company")
	r.NoError(err)
	client := fakeHTTPClient(t, func(req *http.Request) (int, string) {
		r.Equal("Token account-token", req.Header.Get("Authorization"))
		r.Equal(http.MethodPost, req.Method)
		r.Equal("/api/company/", req.URL.Path)
		payload := map[string]any{}
		r.NoError(json.NewDecoder(req.Body).Decode(&payload))
		r.Equal(wantName, payload["name"])
		r.Equal(true, payload["is_supplier"])
		return http.StatusCreated, fmtJSON(testenvRecord{PK: 55, Name: wantName})
	})
	env := &Environment{BaseURL: "http://inventree.test", httpClient: client}

	record, err := env.CreateMutableCompany(ctx, account, run, "company")

	r.NoError(err)
	r.Equal(55, record.ID)
	r.Equal(wantName, record.Name)
}

func TestEnsureFixtureCreatesRequestedRunPrefixedFixtureAndDependencies(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	ctx := context.Background()

	nextID := 100
	starts := 0
	cleanups := 0
	records := map[string]testenvRecord{}
	var created []string
	client := fakeHTTPClient(t, func(req *http.Request) (int, string) {
		r.Equal("Token account-token", req.Header.Get("Authorization"))
		switch req.Method {
		case http.MethodGet:
			if req.URL.RawQuery == "" {
				for _, record := range records {
					if strings.HasSuffix(req.URL.Path, "/"+itoa(record.PK)+"/") {
						return http.StatusOK, fmtJSON(record)
					}
				}
				return http.StatusNotFound, `{}`
			}
			name := req.URL.Query().Get("name")
			if name == "" {
				name = req.URL.Query().Get("SKU")
			}
			if name == "" {
				name = req.URL.Query().Get("reference")
			}
			if record, ok := records[name]; ok {
				return http.StatusOK, fmtJSON(testenvListResponse{Results: []testenvRecord{record}})
			}
			return http.StatusOK, `{"results":[]}`
		case http.MethodPost:
			payload := map[string]any{}
			r.NoError(json.NewDecoder(req.Body).Decode(&payload))
			createdName, ok := payload["name"].(string)
			if !ok || createdName == "" {
				createdName, ok = payload["SKU"].(string)
			}
			if !ok || createdName == "" {
				createdName, ok = payload["reference"].(string)
			}
			r.True(ok)
			nextID++
			record := testenvRecord{PK: nextID}
			data, err := json.Marshal(payload)
			r.NoError(err)
			r.NoError(json.Unmarshal(data, &record))
			record.PK = nextID
			if record.Name == "" {
				record.Name = createdName
			}
			records[createdName] = record
			created = append(created, createdName)
			return http.StatusCreated, fmtJSON(record)
		default:
			return http.StatusMethodNotAllowed, `{}`
		}
	})
	shared, err := startSharedInvenTree(ctx, Options{HTTPClient: client}, func(context.Context, Options) (*Environment, CleanupFunc, error) {
		starts++
		return &Environment{BaseURL: "http://inventree.test", Token: "test-token"}, func() error {
			cleanups++
			return nil
		}, nil
	})
	r.NoError(err)
	r.NotNil(shared)
	r.Equal(1, starts)

	run, err := shared.NewRun(t)
	r.NoError(err)
	accountName, err := run.Name("user")
	r.NoError(err)
	account := &Account{Username: accountName, Token: "account-token", Role: AccountAdmin, run: run}
	categoryName, err := run.Name("category")
	r.NoError(err)
	locationName, err := run.Name("location")
	r.NoError(err)
	partName, err := run.Name("part")
	r.NoError(err)
	supplierName, err := run.Name("supplier")
	r.NoError(err)
	manufacturerName, err := run.Name("manufacturer")
	r.NoError(err)
	supplierPartName, err := run.Name("supplierpart")
	r.NoError(err)
	assemblyName, err := run.Name("assembly")
	r.NoError(err)
	bomName, err := run.Name("bom")
	r.NoError(err)

	category, err := shared.EnsureFixture(ctx, account, run, FixtureCategory)
	r.NoError(err)
	r.Equal(categoryName, category.Name)
	r.NotZero(category.ID)
	r.Equal([]string{categoryName}, created)

	manufacturer, err := shared.EnsureFixture(ctx, account, run, FixtureManufacturer)
	r.NoError(err)
	r.Equal(manufacturerName, manufacturer.Name)
	r.NotZero(manufacturer.ID)

	supplierPart, err := shared.EnsureFixture(ctx, account, run, FixtureSupplierPart)
	r.NoError(err)
	r.Equal(supplierPartName, supplierPart.Name)
	r.NotZero(supplierPart.ID)
	r.ElementsMatch([]string{
		categoryName,
		locationName,
		partName,
		supplierName,
		manufacturerName,
		supplierPartName,
	}, created)

	bom, err := shared.EnsureFixture(ctx, account, run, FixtureBOM)
	r.NoError(err)
	r.Equal(bomName, bom.Name)
	r.NotZero(bom.ID)
	r.ElementsMatch([]string{
		categoryName,
		locationName,
		partName,
		supplierName,
		manufacturerName,
		supplierPartName,
		assemblyName,
		bomName,
	}, created)

	r.NoError(shared.Close(ctx))
	r.Equal(1, cleanups)
}

func TestEnsureFixtureRejectsMutatedExistingFixture(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	ctx := context.Background()

	run, err := newRun("ABC123", "TESTENV", "TESTENSUREREJECTS")
	r.NoError(err)
	categoryName, err := run.Name("category")
	r.NoError(err)

	records := map[string]testenvRecord{
		categoryName: {
			PK:          10,
			Name:        categoryName,
			Description: "operator changed this fixture",
		},
	}
	client := fakeHTTPClient(t, func(req *http.Request) (int, string) {
		r.Equal("Token account-token", req.Header.Get("Authorization"))
		switch req.Method {
		case http.MethodGet:
			if req.URL.RawQuery == "" {
				return http.StatusOK, fmtJSON(records[categoryName])
			}
			return http.StatusOK, fmtJSON(testenvListResponse{Results: []testenvRecord{records[categoryName]}})
		default:
			return http.StatusMethodNotAllowed, `{}`
		}
	})
	shared, err := startSharedInvenTree(ctx, Options{HTTPClient: client}, func(context.Context, Options) (*Environment, CleanupFunc, error) {
		return &Environment{BaseURL: "http://inventree.test", Token: "test-token"}, func() error {
			return nil
		}, nil
	})
	r.NoError(err)
	r.NotNil(shared)
	accountName, err := run.Name("user")
	r.NoError(err)
	account := &Account{Username: accountName, Token: "account-token", Role: AccountAdmin, run: run}

	fixture, err := shared.EnsureFixture(ctx, account, run, FixtureCategory)

	r.Error(err)
	r.ErrorContains(err, "category fixture description")
	r.Zero(fixture)
}

func TestEnsureFixtureRejectsUnsupportedKind(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	ctx := context.Background()

	run, err := newRun("ABC123", "TESTENV", "TESTUNSUPPORTEDFIXTURE")
	r.NoError(err)
	accountName, err := run.Name("user")
	r.NoError(err)
	account := &Account{Username: accountName, Token: "account-token", Role: AccountAdmin, run: run}
	env := &Environment{BaseURL: "http://inventree.test", httpClient: fakeHTTPClient(t, func(*http.Request) (int, string) {
		return http.StatusOK, `{"results":[]}`
	})}

	fixture, err := env.EnsureFixture(ctx, account, run, FixtureKind("unknown"))

	r.ErrorContains(err, "unsupported fixture kind")
	r.Zero(fixture)
}

func TestRandomTestPasswordProducesStrongOpaqueValue(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	first, err := randomTestPassword()
	r.NoError(err)
	second, err := randomTestPassword()
	r.NoError(err)

	r.Contains(first, "InvenTree-Test-")
	r.Contains(first, "-Passw0rd!")
	r.NotEqual(first, second)
}

func TestRunScopedHelpersRejectForeignAccount(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	ctx := context.Background()

	owningRun, err := newRun("ABC123", "TESTENV", "OWNER")
	r.NoError(err)
	foreignRun, err := newRun("ABC123", "TESTENV", "FOREIGN")
	r.NoError(err)
	username, err := owningRun.Name("user")
	r.NoError(err)
	account := &Account{Username: username, Token: "account-token", Role: AccountAdmin, run: owningRun}
	env := &Environment{BaseURL: "http://inventree.test", Token: "test-token", httpClient: fakeHTTPClient(t, func(*http.Request) (int, string) {
		t.Fatal("foreign account must be rejected before API calls")
		return http.StatusInternalServerError, `{}`
	})}

	_, err = env.EnsureFixture(ctx, account, foreignRun, FixtureCategory)
	r.ErrorContains(err, "belongs to run prefix")
	_, err = env.CreateMutableCompany(ctx, account, foreignRun, "company")
	r.ErrorContains(err, "belongs to run prefix")
}

func TestLoopbackPortBinding(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	hostConfig := &container.HostConfig{}
	loopbackPortBinding(defaultWebPort)(hostConfig)

	bindings := hostConfig.PortBindings[dockernetwork.MustParsePort(defaultWebPort)]
	r.Len(bindings, 1)
	r.Equal(netip.MustParseAddr("127.0.0.1"), bindings[0].HostIP)
	r.Empty(bindings[0].HostPort)
}

func TestTestEnvironmentSmallHelpers(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	cleanupCalls := 0
	CleanupForTest(t, func() error {
		cleanupCalls++
		return nil
	})()
	r.Equal(1, cleanupCalls)
	CleanupForTest(t, nil)()

	env := inventreeContainerEnv("db-host", "cache-host")
	r.Equal("db-host", env["INVENTREE_DB_HOST"])
	r.Equal("cache-host", env["INVENTREE_CACHE_HOST"])
	r.Equal(defaultAdminUser, env["INVENTREE_ADMIN_USER"])
	r.Equal(defaultAdminEmail, env["INVENTREE_ADMIN_EMAIL"])

	r.Len(appendContainerLogConsumer(nil, "inventree", func(string, string, string) {}), 1)
	r.Empty(appendContainerLogConsumer(nil, "inventree", nil))
	r.NoError((&Environment{}).Close(context.Background()))
}

func TestDefaultTestOptionsFiltersStartupNoise(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	var got []string
	logf := filteredContainerLogf(func(format string, args ...any) {
		got = append(got, fmt.Sprintf(format, args...))
	})
	r.NotNil(logf)
	r.Nil(filteredContainerLogf(nil))

	logf("postgres", "stderr", "ERROR: relation \"common_setting\" does not exist")
	logf("postgres", "stderr", "STATEMENT: SELECT * FROM common_setting")
	logf("inventree", "stdout", "database migrations completed")
	logf("inventree", "stdout", "Could not detect git information.")
	logf("inventree", "stdout", "ready")

	r.Equal([]string{
		"Dropped 2 migration error logs",
		"container[inventree][stdout] database migrations completed",
		"container[inventree][stdout] ready",
	}, got)
}

func TestContainerLogConsumerForwardsNamedLines(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	var got []string
	consumer := containerLogConsumer{
		name: "inventree",
		logf: func(container string, stream string, line string) {
			got = append(got, container+" "+stream+" "+line)
		},
	}

	consumer.Accept(testcontainers.Log{
		LogType: testcontainers.StderrLog,
		Content: []byte("first line\nsecond line\n"),
	})
	consumer.Accept(testcontainers.Log{
		LogType: testcontainers.StdoutLog,
		Content: []byte("\n"),
	})

	r.Equal([]string{
		"inventree stderr first line",
		"inventree stderr second line",
	}, got)
}

func TestSynchronizedContainerLogfSerializesCalls(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	var got []string
	logf := synchronizedContainerLogf(func(container string, stream string, line string) {
		got = append(got, container+" "+stream+" "+line)
	})

	r.NotNil(logf)
	logf("postgres", "stdout", "ready")
	logf("redis", "stderr", "warning")

	r.Equal([]string{
		"postgres stdout ready",
		"redis stderr warning",
	}, got)
	r.Nil(synchronizedContainerLogf(nil))
}

func TestHTTPHelpersFetchVersionCreateAndProveToken(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	ctx := context.Background()

	client := fakeHTTPClient(t, func(req *http.Request) (int, string) {
		switch req.URL.Path {
		case "/api/version/":
			assertBasicAuth(t, req)
			return http.StatusOK, `{"version":{"server":"1.4.0","api":511}}`
		case "/api/user/me/token/":
			assertBasicAuth(t, req)
			r.Equal(defaultTokenName, req.URL.Query().Get("name"))
			return http.StatusOK, `{"token":"test-token"}`
		case "/api/user/me/":
			r.Equal("Token test-token", req.Header.Get("Authorization"))
			return http.StatusOK, `{"username":"admin"}`
		default:
			return http.StatusNotFound, `{}`
		}
	})

	version, err := fetchVersion(ctx, client, "http://inventree.test", defaultAdminUser, defaultAdminPassword)
	r.NoError(err)
	r.Equal(DefaultVersion, version.Version.Server)
	r.Equal(511, version.Version.API)

	token, err := createToken(ctx, client, "http://inventree.test", defaultAdminUser, defaultAdminPassword)
	r.NoError(err)
	r.Equal("test-token", token)

	r.NoError(proveToken(ctx, "http://inventree.test", token, client))
}

func TestDoJSONRejectsNonSuccessAndInvalidJSON(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	ctx := context.Background()

	client := fakeHTTPClient(t, func(req *http.Request) (int, string) {
		switch req.URL.Path {
		case "/status":
			return http.StatusTeapot, "nope"
		case "/invalid":
			return http.StatusOK, "{"
		default:
			return http.StatusNotFound, "{}"
		}
	})

	statusReq, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://inventree.test/status", nil)
	r.NoError(err)
	r.ErrorContains(doJSON(client, statusReq, &map[string]any{}), "GET /status returned 418")

	invalidReq, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://inventree.test/invalid", nil)
	r.NoError(err)
	r.ErrorContains(doJSON(client, invalidReq, &map[string]any{}), "decode response")
}

func assertBasicAuth(t *testing.T, req *http.Request) {
	t.Helper()
	r := require.New(t)

	username, password, ok := req.BasicAuth()
	r.True(ok)
	r.Equal(defaultAdminUser, username)
	r.Equal(defaultAdminPassword, password)
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func fakeHTTPClient(t *testing.T, handler func(*http.Request) (int, string)) *http.Client {
	t.Helper()

	return &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			status, body := handler(req)
			return &http.Response{
				StatusCode: status,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(body)),
				Request:    req,
			}, nil
		}),
	}
}

func fmtJSON(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return string(data)
}

func itoa(value int) string {
	return strconv.Itoa(value)
}
