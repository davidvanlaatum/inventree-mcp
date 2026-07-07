package testenv

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
)

var runNamePattern = regexp.MustCompile(`^[A-Za-z0-9]+$`)

type SharedInvenTree struct {
	env     *Environment
	cleanup CleanupFunc
	runID   string
}

type AccountRole string

const AccountAdmin AccountRole = "admin"

type Account struct {
	Username string
	Token    string
	Role     AccountRole
	run      *Run
}

type FixtureKind string

const (
	FixtureCategory      FixtureKind = "category"
	FixtureLocation      FixtureKind = "location"
	FixtureSupplier      FixtureKind = "supplier"
	FixtureManufacturer  FixtureKind = "manufacturer"
	FixturePart          FixtureKind = "part"
	FixtureAssemblyPart  FixtureKind = "assembly_part"
	FixtureSupplierPart  FixtureKind = "supplier_part"
	FixturePurchaseOrder FixtureKind = "purchase_order"
	FixtureBOM           FixtureKind = "bom"
)

type FixtureRecord struct {
	ID   int
	Name string
}

type Run struct {
	ID      string
	Package string
	Test    string
	Prefix  string
}

type MutableRecord struct {
	ID   int
	Name string
}

type testenvRecord struct {
	PK              int     `json:"pk"`
	Username        string  `json:"username"`
	Name            string  `json:"name"`
	Description     string  `json:"description"`
	Currency        string  `json:"currency"`
	Active          bool    `json:"active"`
	IsSupplier      bool    `json:"is_supplier"`
	IsManufacturer  bool    `json:"is_manufacturer"`
	IsCustomer      bool    `json:"is_customer"`
	Structural      bool    `json:"structural"`
	External        bool    `json:"external"`
	Category        int     `json:"category"`
	DefaultLocation int     `json:"default_location"`
	Assembly        bool    `json:"assembly"`
	Component       bool    `json:"component"`
	Purchaseable    bool    `json:"purchaseable"`
	Salable         bool    `json:"salable"`
	Trackable       bool    `json:"trackable"`
	Virtual         bool    `json:"virtual"`
	Part            int     `json:"part"`
	Supplier        int     `json:"supplier"`
	SKU             string  `json:"SKU"`
	SubPart         int     `json:"sub_part"`
	Reference       string  `json:"reference"`
	Quantity        float64 `json:"quantity"`
}

type testenvListResponse struct {
	Results []testenvRecord `json:"results"`
}

func StartSharedInvenTree(ctx context.Context, opts Options) (*SharedInvenTree, error) {
	return startSharedInvenTree(ctx, opts, Start)
}

func startSharedInvenTree(
	ctx context.Context,
	opts Options,
	start func(context.Context, Options) (*Environment, CleanupFunc, error),
) (*SharedInvenTree, error) {
	if start == nil {
		start = Start
	}
	env, cleanup, err := start(ctx, opts)
	if err != nil {
		return nil, err
	}
	if env != nil && env.httpClient == nil {
		env.httpClient = opts.HTTPClient
	}
	return &SharedInvenTree{
		env:     env,
		cleanup: cleanup,
		runID:   newRunID(),
	}, nil
}

func (s *SharedInvenTree) Environment() *Environment {
	if s == nil {
		return nil
	}
	return s.env
}

func (s *SharedInvenTree) Close(ctx context.Context) error {
	if s == nil || s.cleanup == nil {
		return nil
	}
	return s.cleanup()
}

func (s *SharedInvenTree) Account(ctx context.Context, run *Run, role AccountRole) (*Account, error) {
	if s == nil || s.env == nil {
		return nil, errors.New("shared InvenTree environment is required")
	}
	return s.env.Account(ctx, run, role)
}

func (e *Environment) Account(ctx context.Context, run *Run, role AccountRole) (*Account, error) {
	if role != AccountAdmin {
		return nil, fmt.Errorf("unsupported test account role %q", role)
	}
	return e.createTestAccount(ctx, run, role)
}

func (s *SharedInvenTree) Client(account *Account) (*inventree.Client, error) {
	if s == nil || s.env == nil {
		return nil, errors.New("shared InvenTree environment is required")
	}
	return s.env.Client(account)
}

func (e *Environment) Client(account *Account) (*inventree.Client, error) {
	if e == nil {
		return nil, errors.New("InvenTree environment is required")
	}
	if account == nil {
		return nil, errors.New("test account is required")
	}
	if account.Token == "" {
		return nil, fmt.Errorf("test account %q has empty token", account.Username)
	}
	return e.clientWithToken(account.Token, nil)
}

func (s *SharedInvenTree) EnsureFixture(ctx context.Context, account *Account, run *Run, kind FixtureKind) (FixtureRecord, error) {
	if s == nil || s.env == nil {
		return FixtureRecord{}, errors.New("shared InvenTree environment is required")
	}
	return s.env.EnsureFixture(ctx, account, run, kind)
}

func (e *Environment) EnsureFixture(ctx context.Context, account *Account, run *Run, kind FixtureKind) (FixtureRecord, error) {
	if err := validateAccountRun(account, run); err != nil {
		return FixtureRecord{}, err
	}
	invClient, err := e.Client(account)
	if err != nil {
		return FixtureRecord{}, err
	}
	return ensureFixture(ctx, e, invClient, account, run, kind)
}

func (s *SharedInvenTree) NewRun(tb testing.TB) (*Run, error) {
	if s == nil {
		return nil, errors.New("shared InvenTree environment is nil")
	}
	if tb == nil {
		return nil, errors.New("testing handle is required")
	}
	tb.Helper()

	pkg := callerPackage()
	rawTest := tb.Name()
	test := sanitizeRunSegment(rawTest)
	if test == "" {
		return nil, errors.New("test name produced empty run segment")
	}
	if s.runID == "" {
		s.runID = newRunID()
	}
	return newRun(s.runID, pkg, test, rawTest)
}

func (r *Run) RequireOwnedName(name string) error {
	if r == nil {
		return errors.New("run is required")
	}
	if r.Prefix == "" {
		return errors.New("run prefix is required")
	}
	if !strings.HasPrefix(name, r.Prefix) {
		return fmt.Errorf("record name %q does not use current run prefix %q", name, r.Prefix)
	}
	return nil
}

func (r *Run) Name(suffix string) (string, error) {
	if r == nil {
		return "", errors.New("run is required")
	}
	suffix = sanitizeRunSegment(suffix)
	if suffix == "" {
		return "", errors.New("record suffix produced empty run segment")
	}
	return r.Prefix + suffix, nil
}

func ValidateMutableRecords(run *Run, records []MutableRecord) error {
	if run == nil {
		return errors.New("run is required")
	}
	var errs []error
	for _, record := range records {
		if err := run.RequireOwnedName(record.Name); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (e *Environment) CreateMutableCompany(ctx context.Context, account *Account, run *Run, suffix string) (MutableRecord, error) {
	if e == nil {
		return MutableRecord{}, errors.New("InvenTree environment is required")
	}
	if err := validateAccountRun(account, run); err != nil {
		return MutableRecord{}, err
	}
	name, err := run.Name(suffix)
	if err != nil {
		return MutableRecord{}, err
	}
	invClient, err := e.Client(account)
	if err != nil {
		return MutableRecord{}, err
	}
	req, err := invClient.NewRequest(ctx, http.MethodPost, "/api/company/", nil, map[string]any{
		"name":            name,
		"description":     "Mutable integration test company",
		"currency":        "USD",
		"active":          true,
		"is_supplier":     true,
		"is_manufacturer": false,
		"is_customer":     false,
	})
	if err != nil {
		return MutableRecord{}, err
	}
	var created testenvRecord
	if err := invClient.DoJSON(req, &created); err != nil {
		return MutableRecord{}, err
	}
	record := MutableRecord{ID: created.PK, Name: created.Name}
	if err := run.RequireOwnedName(record.Name); err != nil {
		return MutableRecord{}, err
	}
	if record.ID == 0 {
		return MutableRecord{}, fmt.Errorf("created mutable company %q returned empty ID", record.Name)
	}
	return record, nil
}

func (e *Environment) createTestAccount(ctx context.Context, run *Run, role AccountRole) (*Account, error) {
	if e == nil {
		return nil, errors.New("InvenTree environment is required")
	}
	if run == nil {
		return nil, errors.New("run is required")
	}
	username, err := run.Name("user")
	if err != nil {
		return nil, err
	}
	adminClient, err := e.client()
	if err != nil {
		return nil, err
	}
	user, err := getOrCreateTestUser(ctx, adminClient, username)
	if err != nil {
		return nil, err
	}

	password, err := randomTestPassword()
	if err != nil {
		return nil, err
	}
	req, err := adminClient.NewRequest(ctx, http.MethodPut, fmt.Sprintf("/api/user/%d/set-password/", user.PK), nil, map[string]any{
		"password":         password,
		"override_warning": true,
	})
	if err != nil {
		return nil, err
	}
	if err := adminClient.DoJSON(req, nil); err != nil {
		return nil, fmt.Errorf("set password for test account %q: %w", username, err)
	}
	token, err := createToken(ctx, e.httpClient, e.BaseURL, username, password)
	if err != nil {
		return nil, fmt.Errorf("create token for test account %q: %w", username, err)
	}
	return &Account{
		Username: username,
		Token:    token,
		Role:     role,
		run:      run,
	}, nil
}

func getOrCreateTestUser(ctx context.Context, adminClient *inventree.Client, username string) (testenvRecord, error) {
	query := url.Values{
		"limit":  []string{"10"},
		"search": []string{username},
	}
	req, err := adminClient.NewRequest(ctx, http.MethodGet, "/api/user/", query, nil)
	if err != nil {
		return testenvRecord{}, err
	}
	var list testenvListResponse
	if err := adminClient.DoJSON(req, &list); err != nil {
		return testenvRecord{}, fmt.Errorf("lookup test account %q: %w", username, err)
	}
	for _, user := range list.Results {
		if user.Username == username {
			if user.PK == 0 {
				return testenvRecord{}, fmt.Errorf("test account %q lookup returned empty ID", username)
			}
			return user, nil
		}
	}

	req, err = adminClient.NewRequest(ctx, http.MethodPost, "/api/user/", nil, map[string]any{
		"username":     username,
		"first_name":   "Integration",
		"last_name":    "Test",
		"email":        strings.ToLower(username) + "@example.test",
		"is_staff":     true,
		"is_superuser": true,
		"is_active":    true,
	})
	if err != nil {
		return testenvRecord{}, err
	}
	var created testenvRecord
	if err := adminClient.DoJSON(req, &created); err != nil {
		return testenvRecord{}, fmt.Errorf("create test account %q: %w", username, err)
	}
	if created.PK == 0 || created.Username != username {
		return testenvRecord{}, fmt.Errorf("create test account %q returned incomplete record", username)
	}
	return created, nil
}

func ensureFixture(
	ctx context.Context,
	env *Environment,
	invClient *inventree.Client,
	account *Account,
	run *Run,
	kind FixtureKind,
) (FixtureRecord, error) {
	fixtureName := func(suffix string) (string, error) {
		if run == nil {
			return "", errors.New("run is required")
		}
		return run.Name(suffix)
	}

	switch kind {
	case FixtureCategory:
		name, err := fixtureName("category")
		if err != nil {
			return FixtureRecord{}, err
		}
		return getOrCreateRecord(ctx, invClient, "/api/part/category/", name, map[string]any{
			"name":        name,
			"description": "Run-scoped integration fixture category",
			"structural":  false,
		}, validateFields("category fixture", map[string]any{
			"name":        name,
			"description": "Run-scoped integration fixture category",
			"structural":  false,
		}))
	case FixtureLocation:
		name, err := fixtureName("location")
		if err != nil {
			return FixtureRecord{}, err
		}
		return getOrCreateRecord(ctx, invClient, "/api/stock/location/", name, map[string]any{
			"name":        name,
			"description": "Run-scoped integration fixture location",
			"structural":  false,
			"external":    false,
		}, validateFields("location fixture", map[string]any{
			"name":        name,
			"description": "Run-scoped integration fixture location",
			"structural":  false,
			"external":    false,
		}))
	case FixtureSupplier:
		name, err := fixtureName("supplier")
		if err != nil {
			return FixtureRecord{}, err
		}
		return getOrCreateRecord(ctx, invClient, "/api/company/", name, map[string]any{
			"name":            name,
			"description":     "Run-scoped integration fixture supplier",
			"currency":        "USD",
			"active":          true,
			"is_supplier":     true,
			"is_manufacturer": false,
			"is_customer":     false,
		}, validateFields("supplier fixture", map[string]any{
			"name":            name,
			"description":     "Run-scoped integration fixture supplier",
			"currency":        "USD",
			"active":          true,
			"is_supplier":     true,
			"is_manufacturer": false,
			"is_customer":     false,
		}))
	case FixtureManufacturer:
		name, err := fixtureName("manufacturer")
		if err != nil {
			return FixtureRecord{}, err
		}
		return getOrCreateRecord(ctx, invClient, "/api/company/", name, map[string]any{
			"name":            name,
			"description":     "Run-scoped integration fixture manufacturer",
			"currency":        "USD",
			"active":          true,
			"is_supplier":     false,
			"is_manufacturer": true,
			"is_customer":     false,
		}, validateFields("manufacturer fixture", map[string]any{
			"name":            name,
			"description":     "Run-scoped integration fixture manufacturer",
			"currency":        "USD",
			"active":          true,
			"is_supplier":     false,
			"is_manufacturer": true,
			"is_customer":     false,
		}))
	case FixturePart:
		name, err := fixtureName("part")
		if err != nil {
			return FixtureRecord{}, err
		}
		category, err := ensureFixture(ctx, env, invClient, account, run, FixtureCategory)
		if err != nil {
			return FixtureRecord{}, fmt.Errorf("ensure category fixture for part: %w", err)
		}
		location, err := ensureFixture(ctx, env, invClient, account, run, FixtureLocation)
		if err != nil {
			return FixtureRecord{}, fmt.Errorf("ensure location fixture for part: %w", err)
		}
		return getOrCreateRecord(ctx, invClient, "/api/part/", name, map[string]any{
			"name":             name,
			"description":      "Run-scoped integration fixture part",
			"category":         category.ID,
			"default_location": location.ID,
			"active":           true,
			"component":        true,
			"purchaseable":     true,
			"salable":          false,
			"assembly":         false,
			"trackable":        false,
			"virtual":          false,
		}, validateFields("part fixture", map[string]any{
			"name":             name,
			"description":      "Run-scoped integration fixture part",
			"category":         category.ID,
			"default_location": location.ID,
			"active":           true,
			"component":        true,
			"purchaseable":     true,
			"salable":          false,
			"assembly":         false,
			"trackable":        false,
			"virtual":          false,
		}))
	case FixtureAssemblyPart:
		name, err := fixtureName("assembly")
		if err != nil {
			return FixtureRecord{}, err
		}
		category, err := ensureFixture(ctx, env, invClient, account, run, FixtureCategory)
		if err != nil {
			return FixtureRecord{}, fmt.Errorf("ensure category fixture for assembly: %w", err)
		}
		location, err := ensureFixture(ctx, env, invClient, account, run, FixtureLocation)
		if err != nil {
			return FixtureRecord{}, fmt.Errorf("ensure location fixture for assembly: %w", err)
		}
		return getOrCreateRecord(ctx, invClient, "/api/part/", name, map[string]any{
			"name":             name,
			"description":      "Run-scoped integration fixture assembly",
			"category":         category.ID,
			"default_location": location.ID,
			"active":           true,
			"component":        false,
			"purchaseable":     false,
			"salable":          false,
			"assembly":         true,
			"trackable":        false,
			"virtual":          false,
		}, validateFields("assembly fixture", map[string]any{
			"name":             name,
			"description":      "Run-scoped integration fixture assembly",
			"category":         category.ID,
			"default_location": location.ID,
			"active":           true,
			"component":        false,
			"purchaseable":     false,
			"salable":          false,
			"assembly":         true,
			"trackable":        false,
			"virtual":          false,
		}))
	case FixtureSupplierPart:
		name, err := fixtureName("supplierpart")
		if err != nil {
			return FixtureRecord{}, err
		}
		part, err := ensureFixture(ctx, env, invClient, account, run, FixturePart)
		if err != nil {
			return FixtureRecord{}, fmt.Errorf("ensure part fixture for supplier part: %w", err)
		}
		supplier, err := ensureFixture(ctx, env, invClient, account, run, FixtureSupplier)
		if err != nil {
			return FixtureRecord{}, fmt.Errorf("ensure supplier fixture for supplier part: %w", err)
		}
		return getOrCreateRecord(ctx, invClient, "/api/company/part/", name, map[string]any{
			"part":        part.ID,
			"supplier":    supplier.ID,
			"SKU":         name,
			"description": "Run-scoped integration fixture supplier part",
			"active":      true,
			"primary":     true,
		}, validateFields("supplier part fixture", map[string]any{
			"part":     part.ID,
			"supplier": supplier.ID,
			"SKU":      name,
			"active":   true,
		}))
	case FixturePurchaseOrder:
		supplier, err := ensureFixture(ctx, env, invClient, account, run, FixtureSupplier)
		if err != nil {
			return FixtureRecord{}, fmt.Errorf("ensure supplier fixture for purchase order: %w", err)
		}
		name := "PO-" + strconv.Itoa(supplier.ID)
		return getOrCreateRecord(ctx, invClient, "/api/order/po/", name, map[string]any{
			"reference":   name,
			"supplier":    supplier.ID,
			"description": "Run-scoped integration fixture purchase order",
		}, validateFields("purchase order fixture", map[string]any{
			"reference": name,
			"supplier":  supplier.ID,
		}))
	case FixtureBOM:
		name, err := fixtureName("bom")
		if err != nil {
			return FixtureRecord{}, err
		}
		assemblyPart, err := ensureFixture(ctx, env, invClient, account, run, FixtureAssemblyPart)
		if err != nil {
			return FixtureRecord{}, fmt.Errorf("ensure assembly fixture for BOM: %w", err)
		}
		part, err := ensureFixture(ctx, env, invClient, account, run, FixturePart)
		if err != nil {
			return FixtureRecord{}, fmt.Errorf("ensure part fixture for BOM: %w", err)
		}
		return getOrCreateRecord(ctx, invClient, "/api/bom/", name, map[string]any{
			"part":      assemblyPart.ID,
			"sub_part":  part.ID,
			"reference": name,
			"quantity":  1,
		}, validateFields("BOM fixture", map[string]any{
			"part":      assemblyPart.ID,
			"sub_part":  part.ID,
			"reference": name,
			"quantity":  float64(1),
		}))
	default:
		return FixtureRecord{}, fmt.Errorf("unsupported fixture kind %q", kind)
	}
}

func getOrCreateRecord(
	ctx context.Context,
	client *inventree.Client,
	apiPath string,
	name string,
	payload map[string]any,
	validate func(testenvRecord) error,
) (FixtureRecord, error) {
	query := url.Values{
		"limit": []string{"10"},
	}
	switch apiPath {
	case "/api/company/part/":
		query.Set("SKU", name)
	case "/api/bom/", "/api/order/po/":
		query.Set("reference", name)
	default:
		query.Set("name", name)
	}
	req, err := client.NewRequest(ctx, http.MethodGet, apiPath, query, nil)
	if err != nil {
		return FixtureRecord{}, err
	}
	var list testenvListResponse
	if err := client.DoJSON(req, &list); err != nil {
		return FixtureRecord{}, err
	}
	for _, record := range list.Results {
		if recordMatchesName(record, name) {
			if validate != nil {
				detail, err := getRecordDetail(ctx, client, apiPath, record.PK)
				if err != nil {
					return FixtureRecord{}, err
				}
				if err := validate(detail); err != nil {
					return FixtureRecord{}, err
				}
			}
			return FixtureRecord{ID: record.PK, Name: name}, nil
		}
	}

	req, err = client.NewRequest(ctx, http.MethodPost, apiPath, nil, payload)
	if err != nil {
		return FixtureRecord{}, err
	}
	var created testenvRecord
	if err := client.DoJSON(req, &created); err != nil {
		return FixtureRecord{}, err
	}
	if created.Name == "" {
		created.Name = name
	}
	if created.PK == 0 || !recordMatchesName(created, name) {
		return FixtureRecord{}, fmt.Errorf("created fixture %q returned incomplete record", name)
	}
	if validate != nil {
		if err := validate(created); err != nil {
			return FixtureRecord{}, err
		}
	}
	return FixtureRecord{ID: created.PK, Name: created.Name}, nil
}

func (e *Environment) client() (*inventree.Client, error) {
	if e == nil {
		return nil, errors.New("InvenTree environment is required")
	}
	return e.clientWithToken(e.Token, nil)
}

func (e *Environment) clientWithToken(token string, httpClient *http.Client) (*inventree.Client, error) {
	if e == nil {
		return nil, errors.New("InvenTree environment is required")
	}
	if token == "" {
		return nil, errors.New("InvenTree token is required")
	}
	if httpClient == nil {
		httpClient = e.httpClient
	}
	return inventree.NewClient(
		inventree.Config{
			BaseURL: e.BaseURL,
			Credential: inventree.Credential{
				Scheme: inventree.AuthSchemeToken,
				Token:  token,
			},
			HTTPClient: httpClient,
		},
	)
}

func getRecordDetail(ctx context.Context, client *inventree.Client, apiPath string, id int) (testenvRecord, error) {
	req, err := client.NewRequest(ctx, http.MethodGet, fmt.Sprintf("%s%d/", apiPath, id), nil, nil)
	if err != nil {
		return testenvRecord{}, err
	}
	var out testenvRecord
	if err := client.DoJSON(req, &out); err != nil {
		return testenvRecord{}, err
	}
	return out, nil
}

func recordMatchesName(record testenvRecord, name string) bool {
	return record.Name == name || record.SKU == name || record.Reference == name
}

func validateFields(label string, want map[string]any) func(testenvRecord) error {
	return func(record testenvRecord) error {
		got := map[string]any{
			"name":             record.Name,
			"description":      record.Description,
			"currency":         record.Currency,
			"active":           record.Active,
			"is_supplier":      record.IsSupplier,
			"is_manufacturer":  record.IsManufacturer,
			"is_customer":      record.IsCustomer,
			"structural":       record.Structural,
			"external":         record.External,
			"category":         record.Category,
			"default_location": record.DefaultLocation,
			"assembly":         record.Assembly,
			"component":        record.Component,
			"purchaseable":     record.Purchaseable,
			"salable":          record.Salable,
			"trackable":        record.Trackable,
			"virtual":          record.Virtual,
			"part":             record.Part,
			"supplier":         record.Supplier,
			"SKU":              record.SKU,
			"sub_part":         record.SubPart,
			"reference":        record.Reference,
			"quantity":         record.Quantity,
		}
		var errs []error
		for key, wantValue := range want {
			if got[key] != wantValue {
				errs = append(errs, fmt.Errorf("%s %s = %v, want %v", label, key, got[key], wantValue))
			}
		}
		return errors.Join(errs...)
	}
}

func validateAccountRun(account *Account, run *Run) error {
	if account == nil {
		return errors.New("test account is required")
	}
	if run == nil {
		return errors.New("run is required")
	}
	if account.run == nil {
		return fmt.Errorf("test account %q is missing its owning run", account.Username)
	}
	if account.run.Prefix != run.Prefix {
		return fmt.Errorf("test account %q belongs to run prefix %q, not %q", account.Username, account.run.Prefix, run.Prefix)
	}
	if err := run.RequireOwnedName(account.Username); err != nil {
		return fmt.Errorf("test account %q is not owned by run: %w", account.Username, err)
	}
	return nil
}

func newRun(runID string, pkg string, test string, rawTest ...string) (*Run, error) {
	if !runNamePattern.MatchString(runID) {
		return nil, fmt.Errorf("run id %q must contain only letters and digits", runID)
	}
	if !runNamePattern.MatchString(pkg) {
		return nil, fmt.Errorf("package segment %q must contain only letters and digits", pkg)
	}
	if !runNamePattern.MatchString(test) {
		return nil, fmt.Errorf("test segment %q must contain only letters and digits", test)
	}
	hashSource := test
	if len(rawTest) > 0 {
		hashSource = rawTest[0]
	}
	return &Run{
		ID:      runID,
		Package: pkg,
		Test:    test,
		Prefix:  "IT_" + runID + "_" + pkg + "_" + runHashSegment(pkg, hashSource) + "_",
	}, nil
}

func newRunID() string {
	var data [4]byte
	if _, err := rand.Read(data[:]); err != nil {
		return fmt.Sprintf("%08X", uint32(time.Now().UnixNano()))
	}
	return strings.ToUpper(hex.EncodeToString(data[:]))
}

func randomTestPassword() (string, error) {
	var data [12]byte
	if _, err := rand.Read(data[:]); err != nil {
		return "", fmt.Errorf("generate test account password: %w", err)
	}
	return "InvenTree-Test-" + hex.EncodeToString(data[:]) + "-Passw0rd!", nil
}

func callerPackage() string {
	_, file, _, ok := runtime.Caller(2)
	if !ok {
		return "unknown"
	}
	return sanitizeRunSegment(path.Base(path.Dir(file)))
}

func sanitizeRunSegment(value string) string {
	var builder strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			builder.WriteRune(r - 'a' + 'A')
		case r >= 'A' && r <= 'Z':
			builder.WriteRune(r)
		case r >= '0' && r <= '9':
			builder.WriteRune(r)
		}
	}
	return builder.String()
}

func runHashSegment(pkg string, test string) string {
	sum := sha256.Sum256([]byte(pkg + "/" + test))
	return strings.ToUpper(hex.EncodeToString(sum[:6]))
}
