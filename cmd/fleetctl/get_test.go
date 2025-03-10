package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ghodss/yaml"

	"github.com/fleetdm/fleet/v4/pkg/spec"
	"github.com/fleetdm/fleet/v4/server/config"
	"github.com/fleetdm/fleet/v4/server/fleet"
	"github.com/fleetdm/fleet/v4/server/ptr"
	"github.com/fleetdm/fleet/v4/server/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var userRoleList = []*fleet.User{
	{
		UpdateCreateTimestamps: fleet.UpdateCreateTimestamps{
			CreateTimestamp: fleet.CreateTimestamp{CreatedAt: time.Now()},
			UpdateTimestamp: fleet.UpdateTimestamp{UpdatedAt: time.Now()},
		},
		ID:         42,
		Name:       "Test Name admin1@example.com",
		Email:      "admin1@example.com",
		GlobalRole: ptr.String(fleet.RoleAdmin),
	},
	{
		UpdateCreateTimestamps: fleet.UpdateCreateTimestamps{
			CreateTimestamp: fleet.CreateTimestamp{CreatedAt: time.Now()},
			UpdateTimestamp: fleet.UpdateTimestamp{UpdatedAt: time.Now()},
		},
		ID:         23,
		Name:       "Test Name2 admin2@example.com",
		Email:      "admin2@example.com",
		GlobalRole: nil,
		Teams: []fleet.UserTeam{
			{
				Team: fleet.Team{
					ID:        1,
					CreatedAt: time.Now(),
					Name:      "team1",
					UserCount: 1,
					HostCount: 1,
				},
				Role: fleet.RoleMaintainer,
			},
		},
	},
}

func TestGetUserRoles(t *testing.T) {
	_, ds := runServerWithMockedDS(t)

	ds.ListUsersFunc = func(ctx context.Context, opt fleet.UserListOptions) ([]*fleet.User, error) {
		return userRoleList, nil
	}

	expectedText := `+-------------------------------+-------------+
|             USER              | GLOBAL ROLE |
+-------------------------------+-------------+
| Test Name admin1@example.com  | admin       |
+-------------------------------+-------------+
| Test Name2 admin2@example.com |             |
+-------------------------------+-------------+
`
	expectedYaml := `---
apiVersion: v1
kind: user_roles
spec:
  roles:
    admin1@example.com:
      global_role: admin
      teams: null
    admin2@example.com:
      global_role: null
      teams:
      - role: maintainer
        team: team1
`
	expectedJson := `{"kind":"user_roles","apiVersion":"v1","spec":{"roles":{"admin1@example.com":{"global_role":"admin","teams":null},"admin2@example.com":{"global_role":null,"teams":[{"team":"team1","role":"maintainer"}]}}}}
`

	assert.Equal(t, expectedText, runAppForTest(t, []string{"get", "user_roles"}))
	assert.YAMLEq(t, expectedYaml, runAppForTest(t, []string{"get", "user_roles", "--yaml"}))
	assert.JSONEq(t, expectedJson, runAppForTest(t, []string{"get", "user_roles", "--json"}))
}

func TestGetTeams(t *testing.T) {
	var expiredBanner strings.Builder
	fleet.WriteExpiredLicenseBanner(&expiredBanner)
	require.Contains(t, expiredBanner.String(), "Your license for Fleet Premium is about to expire")

	testCases := []struct {
		name                    string
		license                 *fleet.LicenseInfo
		shouldHaveExpiredBanner bool
	}{
		{
			"not expired license",
			&fleet.LicenseInfo{Tier: fleet.TierPremium, Expiration: time.Now().Add(24 * time.Hour)},
			false,
		},
		{
			"expired license",
			&fleet.LicenseInfo{Tier: fleet.TierPremium, Expiration: time.Now().Add(-24 * time.Hour)},
			true,
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			license := tt.license
			_, ds := runServerWithMockedDS(t, &service.TestServerOpts{License: license})

			agentOpts := json.RawMessage(`{"config":{"foo":"bar"},"overrides":{"platforms":{"darwin":{"foo":"override"}}}}`)
			additionalQueries := json.RawMessage(`{"foo":"bar"}`)
			ds.ListTeamsFunc = func(ctx context.Context, filter fleet.TeamFilter, opt fleet.ListOptions) ([]*fleet.Team, error) {
				created_at, err := time.Parse(time.RFC3339, "1999-03-10T02:45:06.371Z")
				require.NoError(t, err)
				return []*fleet.Team{
					{
						ID:          42,
						CreatedAt:   created_at,
						Name:        "team1",
						Description: "team1 description",
						UserCount:   99,
						HostCount:   42,
						Config: fleet.TeamConfig{
							Features: fleet.Features{
								EnableHostUsers:         true,
								EnableSoftwareInventory: true,
							},
						},
					},
					{
						ID:          43,
						CreatedAt:   created_at,
						Name:        "team2",
						Description: "team2 description",
						UserCount:   87,
						HostCount:   43,
						Config: fleet.TeamConfig{
							AgentOptions: &agentOpts,
							Features: fleet.Features{
								AdditionalQueries: &additionalQueries,
							},
							MDM: fleet.TeamMDM{
								MacOSUpdates: fleet.MacOSUpdates{
									MinimumVersion: "12.3.1",
									Deadline:       "2021-12-14",
								},
							},
						},
					},
				}, nil
			}

			b, err := ioutil.ReadFile(filepath.Join("testdata", "expectedGetTeamsText.txt"))
			require.NoError(t, err)
			expectedText := string(b)

			b, err = ioutil.ReadFile(filepath.Join("testdata", "expectedGetTeamsYaml.yml"))
			require.NoError(t, err)
			expectedYaml := string(b)

			b, err = ioutil.ReadFile(filepath.Join("testdata", "expectedGetTeamsJson.json"))
			require.NoError(t, err)
			// must read each JSON value separately and compact it
			var buf bytes.Buffer
			dec := json.NewDecoder(bytes.NewReader(b))
			for {
				var raw json.RawMessage
				if err := dec.Decode(&raw); err != nil {
					if err == io.EOF {
						break
					}
					require.NoError(t, err)
				}
				require.NoError(t, json.Compact(&buf, raw))
				buf.WriteByte('\n')
			}
			expectedJson := buf.String()

			if tt.shouldHaveExpiredBanner {
				expectedJson = expiredBanner.String() + expectedJson
				expectedText = expiredBanner.String() + expectedText
			}

			assert.Equal(t, expectedText, runAppForTest(t, []string{"get", "teams"}))
			// cannot use assert.JSONEq like we do for YAML because this is not a
			// single JSON value, it is a list of 2 JSON objects.
			assert.Equal(t, expectedJson, runAppForTest(t, []string{"get", "teams", "--json"}))

			actualYaml := runAppForTest(t, []string{"get", "teams", "--yaml"})
			if tt.shouldHaveExpiredBanner {
				require.True(t, strings.HasPrefix(actualYaml, expiredBanner.String()))
				actualYaml = strings.TrimPrefix(actualYaml, expiredBanner.String())
			}
			assert.YAMLEq(t, expectedYaml, actualYaml)
		})
	}
}

func TestGetTeamsByName(t *testing.T) {
	_, ds := runServerWithMockedDS(t, &service.TestServerOpts{License: &fleet.LicenseInfo{Tier: fleet.TierPremium, Expiration: time.Now().Add(24 * time.Hour)}})

	ds.ListTeamsFunc = func(ctx context.Context, filter fleet.TeamFilter, opt fleet.ListOptions) ([]*fleet.Team, error) {
		require.Equal(t, "test1", opt.MatchQuery)

		created_at, err := time.Parse(time.RFC3339, "1999-03-10T02:45:06.371Z")
		require.NoError(t, err)
		return []*fleet.Team{
			{
				ID:          42,
				CreatedAt:   created_at,
				Name:        "team1",
				Description: "team1 description",
				UserCount:   99,
				HostCount:   43,
			},
		}, nil
	}

	expectedText := `+-----------+------------+------------+
| TEAM NAME | HOST COUNT | USER COUNT |
+-----------+------------+------------+
| team1     |         43 |         99 |
+-----------+------------+------------+
`
	assert.Equal(t, expectedText, runAppForTest(t, []string{"get", "teams", "--name", "test1"}))
}

func TestGetHosts(t *testing.T) {
	_, ds := runServerWithMockedDS(t)

	ds.AppConfigFunc = func(ctx context.Context) (*fleet.AppConfig, error) {
		return &fleet.AppConfig{}, nil
	}

	// this func is called when no host is specified i.e. `fleetctl get hosts --json`
	ds.ListHostsFunc = func(ctx context.Context, filter fleet.TeamFilter, opt fleet.HostListOptions) ([]*fleet.Host, error) {
		additional := json.RawMessage(`{"query1": [{"col1": "val", "col2": 42}]}`)
		hosts := []*fleet.Host{
			{
				UpdateCreateTimestamps: fleet.UpdateCreateTimestamps{
					CreateTimestamp: fleet.CreateTimestamp{CreatedAt: time.Time{}},
					UpdateTimestamp: fleet.UpdateTimestamp{UpdatedAt: time.Time{}},
				},
				HostSoftware:    fleet.HostSoftware{},
				DetailUpdatedAt: time.Time{},
				LabelUpdatedAt:  time.Time{},
				LastEnrolledAt:  time.Time{},
				SeenTime:        time.Time{},
				ComputerName:    "test_host",
				Hostname:        "test_host",
				Additional:      &additional,
			},
			{
				UpdateCreateTimestamps: fleet.UpdateCreateTimestamps{
					CreateTimestamp: fleet.CreateTimestamp{CreatedAt: time.Time{}},
					UpdateTimestamp: fleet.UpdateTimestamp{UpdatedAt: time.Time{}},
				},
				HostSoftware:    fleet.HostSoftware{},
				DetailUpdatedAt: time.Time{},
				LabelUpdatedAt:  time.Time{},
				LastEnrolledAt:  time.Time{},
				SeenTime:        time.Time{},
				ComputerName:    "test_host2",
				Hostname:        "test_host2",
			},
		}
		return hosts, nil
	}

	// these are run when host is specified `fleetctl get hosts --json test_host`
	ds.HostByIdentifierFunc = func(ctx context.Context, identifier string) (*fleet.Host, error) {
		require.NotEmpty(t, identifier)
		return &fleet.Host{
			UpdateCreateTimestamps: fleet.UpdateCreateTimestamps{
				CreateTimestamp: fleet.CreateTimestamp{CreatedAt: time.Time{}},
				UpdateTimestamp: fleet.UpdateTimestamp{UpdatedAt: time.Time{}},
			},
			HostSoftware:    fleet.HostSoftware{},
			DetailUpdatedAt: time.Time{},
			LabelUpdatedAt:  time.Time{},
			LastEnrolledAt:  time.Time{},
			SeenTime:        time.Time{},
			ComputerName:    "test_host",
			Hostname:        "test_host",
		}, nil
	}

	ds.LoadHostSoftwareFunc = func(ctx context.Context, host *fleet.Host, includeCVEScores bool) error {
		return nil
	}
	ds.ListLabelsForHostFunc = func(ctx context.Context, hid uint) ([]*fleet.Label, error) {
		return make([]*fleet.Label, 0), nil
	}
	ds.ListPacksForHostFunc = func(ctx context.Context, hid uint) (packs []*fleet.Pack, err error) {
		return make([]*fleet.Pack, 0), nil
	}
	ds.ListHostBatteriesFunc = func(ctx context.Context, hid uint) (batteries []*fleet.HostBattery, err error) {
		return nil, nil
	}
	defaultPolicyQuery := "select 1 from osquery_info where start_time > 1;"
	ds.ListPoliciesForHostFunc = func(ctx context.Context, host *fleet.Host) ([]*fleet.HostPolicy, error) {
		return []*fleet.HostPolicy{
			{
				PolicyData: fleet.PolicyData{
					ID:          1,
					Name:        "query1",
					Query:       defaultPolicyQuery,
					Description: "Some description",
					AuthorID:    ptr.Uint(1),
					AuthorName:  "Alice",
					AuthorEmail: "alice@example.com",
					Resolution:  ptr.String("Some resolution"),
					TeamID:      ptr.Uint(1),
				},
				Response: "passes",
			},
			{
				PolicyData: fleet.PolicyData{
					ID:          2,
					Name:        "query2",
					Query:       defaultPolicyQuery,
					Description: "",
					AuthorID:    ptr.Uint(1),
					AuthorName:  "Alice",
					AuthorEmail: "alice@example.com",
					Resolution:  nil,
					TeamID:      nil,
				},
				Response: "fails",
			},
		}, nil
	}

	expectedText := `+------+------------+----------+-----------------+---------+
| UUID |  HOSTNAME  | PLATFORM | OSQUERY VERSION | STATUS  |
+------+------------+----------+-----------------+---------+
|      | test_host  |          |                 | offline |
+------+------------+----------+-----------------+---------+
|      | test_host2 |          |                 | offline |
+------+------------+----------+-----------------+---------+
`

	assert.Equal(t, expectedText, runAppForTest(t, []string{"get", "hosts"}))

	_, err := runAppNoChecks([]string{"get", "hosts", "--mdm"})
	require.Error(t, err)
	assert.ErrorContains(t, err, "MDM features aren't turned on")

	_, err = runAppNoChecks([]string{"get", "hosts", "--mdm-pending"})
	require.Error(t, err)
	assert.ErrorContains(t, err, "MDM features aren't turned on")

	jsonPrettify := func(t *testing.T, v string) string {
		var i interface{}
		err := json.Unmarshal([]byte(v), &i)
		require.NoError(t, err)
		indented, err := json.MarshalIndent(i, "", "  ")
		require.NoError(t, err)
		return string(indented)
	}
	yamlPrettify := func(t *testing.T, v string) string {
		var i interface{}
		err := yaml.Unmarshal([]byte(v), &i)
		require.NoError(t, err)
		indented, err := yaml.Marshal(i)
		require.NoError(t, err)
		return string(indented)
	}
	tests := []struct {
		name       string
		goldenFile string
		scanner    func(s string) []string
		prettifier func(t *testing.T, v string) string
		args       []string
	}{
		{
			name:       "get hosts --json",
			goldenFile: "expectedListHostsJson.json",
			scanner: func(s string) []string {
				parts := strings.Split(s, "}\n{")
				return []string{parts[0] + "}", "{" + parts[1]}
			},
			args:       []string{"get", "hosts", "--json"},
			prettifier: jsonPrettify,
		},
		{
			name:       "get hosts --json test_host",
			goldenFile: "expectedHostDetailResponseJson.json",
			scanner:    func(s string) []string { return []string{s} },
			args:       []string{"get", "hosts", "--json", "test_host"},
			prettifier: jsonPrettify,
		},
		{
			name:       "get hosts --yaml",
			goldenFile: "expectedListHostsYaml.yml",
			scanner: func(s string) []string {
				return []string{s}
			},
			args:       []string{"get", "hosts", "--yaml"},
			prettifier: yamlPrettify,
		},
		{
			name:       "get hosts --yaml test_host",
			goldenFile: "expectedHostDetailResponseYaml.yml",
			scanner: func(s string) []string {
				return spec.SplitYaml(s)
			},
			args:       []string{"get", "hosts", "--yaml", "test_host"},
			prettifier: yamlPrettify,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expected, err := ioutil.ReadFile(filepath.Join("testdata", tt.goldenFile))
			require.NoError(t, err)
			expectedResults := tt.scanner(string(expected))
			actualResult := tt.scanner(runAppForTest(t, tt.args))
			require.Equal(t, len(expectedResults), len(actualResult))
			for i := range expectedResults {
				require.Equal(t, tt.prettifier(t, expectedResults[i]), tt.prettifier(t, actualResult[i]))
			}
		})
	}
}

func TestGetHostsMDM(t *testing.T) {
	_, ds := runServerWithMockedDS(t)

	ds.AppConfigFunc = func(ctx context.Context) (*fleet.AppConfig, error) {
		return &fleet.AppConfig{MDM: fleet.MDM{EnabledAndConfigured: true}}, nil
	}

	// this func is called when no host is specified i.e. `fleetctl get hosts --json`
	ds.ListHostsFunc = func(ctx context.Context, filter fleet.TeamFilter, opt fleet.HostListOptions) ([]*fleet.Host, error) {
		additional := json.RawMessage(`{"query1": [{"col1": "val", "col2": 42}]}`)
		hosts := []*fleet.Host{
			{
				UpdateCreateTimestamps: fleet.UpdateCreateTimestamps{
					CreateTimestamp: fleet.CreateTimestamp{CreatedAt: time.Time{}},
					UpdateTimestamp: fleet.UpdateTimestamp{UpdatedAt: time.Time{}},
				},
				HostSoftware:    fleet.HostSoftware{},
				DetailUpdatedAt: time.Time{},
				LabelUpdatedAt:  time.Time{},
				LastEnrolledAt:  time.Time{},
				SeenTime:        time.Time{},
				ComputerName:    "test_host",
				Hostname:        "test_host",
				Additional:      &additional,
			},
			{
				UpdateCreateTimestamps: fleet.UpdateCreateTimestamps{
					CreateTimestamp: fleet.CreateTimestamp{CreatedAt: time.Time{}},
					UpdateTimestamp: fleet.UpdateTimestamp{UpdatedAt: time.Time{}},
				},
				HostSoftware:    fleet.HostSoftware{},
				DetailUpdatedAt: time.Time{},
				LabelUpdatedAt:  time.Time{},
				LastEnrolledAt:  time.Time{},
				SeenTime:        time.Time{},
				ComputerName:    "test_host2",
				Hostname:        "test_host2",
			},
		}
		return hosts, nil
	}

	ds.LoadHostSoftwareFunc = func(ctx context.Context, host *fleet.Host, includeCVEScores bool) error {
		return nil
	}
	ds.ListLabelsForHostFunc = func(ctx context.Context, hid uint) ([]*fleet.Label, error) {
		return make([]*fleet.Label, 0), nil
	}
	ds.ListPacksForHostFunc = func(ctx context.Context, hid uint) (packs []*fleet.Pack, err error) {
		return make([]*fleet.Pack, 0), nil
	}
	ds.ListHostBatteriesFunc = func(ctx context.Context, hid uint) (batteries []*fleet.HostBattery, err error) {
		return nil, nil
	}
	ds.ListPoliciesForHostFunc = func(ctx context.Context, host *fleet.Host) ([]*fleet.HostPolicy, error) {
		return nil, nil
	}

	tests := []struct {
		name       string
		args       []string
		goldenFile string
		wantErr    string
	}{
		{
			name:    "get hosts --mdm --mdm-pending",
			args:    []string{"get", "hosts", "--mdm", "--mdm-pending"},
			wantErr: "cannot use --mdm and --mdm-pending together",
		},
		{
			name:       "get hosts --mdm --json",
			args:       []string{"get", "hosts", "--mdm", "--json"},
			goldenFile: "expectedListHostsMDM.json",
		},
		{
			name:       "get hosts --mdm-pending --yaml",
			args:       []string{"get", "hosts", "--mdm-pending", "--yaml"},
			goldenFile: "expectedListHostsYaml.yml",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := runAppNoChecks(tt.args)
			if tt.wantErr != "" {
				require.Error(t, err)
				require.ErrorContains(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
			}

			if tt.goldenFile != "" {
				expected, err := ioutil.ReadFile(filepath.Join("testdata", tt.goldenFile))
				require.NoError(t, err)
				if ext := filepath.Ext(tt.goldenFile); ext == ".json" {
					// the output of --json is not a json array, but a list of
					// newline-separated json objects. fix that for the assertion,
					// turning it into a JSON array.
					actual := "[" + strings.ReplaceAll(got.String(), "}\n{", "},{") + "]"
					require.JSONEq(t, string(expected), actual)
				} else {
					require.YAMLEq(t, string(expected), got.String())
				}
			}
		})
	}
}

func TestGetConfig(t *testing.T) {
	_, ds := runServerWithMockedDS(t)

	ds.AppConfigFunc = func(ctx context.Context) (*fleet.AppConfig, error) {
		return &fleet.AppConfig{
			Features:              fleet.Features{EnableHostUsers: true},
			VulnerabilitySettings: fleet.VulnerabilitySettings{DatabasesPath: "/some/path"},
		}, nil
	}

	t.Run("AppConfig", func(t *testing.T) {
		b, err := os.ReadFile(filepath.Join("testdata", "expectedGetConfigAppConfigYaml.yml"))
		require.NoError(t, err)
		expectedYaml := string(b)

		b, err = os.ReadFile(filepath.Join("testdata", "expectedGetConfigAppConfigJson.json"))
		require.NoError(t, err)
		expectedJson := string(b)

		assert.YAMLEq(t, expectedYaml, runAppForTest(t, []string{"get", "config"}))
		assert.YAMLEq(t, expectedYaml, runAppForTest(t, []string{"get", "config", "--yaml"}))
		assert.JSONEq(t, expectedJson, runAppForTest(t, []string{"get", "config", "--json"}))
	})

	t.Run("IncludeServerConfig", func(t *testing.T) {
		b, err := os.ReadFile(filepath.Join("testdata", "expectedGetConfigIncludeServerConfigYaml.yml"))
		require.NoError(t, err)
		expectedYAML := string(b)

		b, err = os.ReadFile(filepath.Join("testdata", "expectedGetConfigIncludeServerConfigJson.json"))
		require.NoError(t, err)
		expectedJSON := string(b)

		assert.YAMLEq(t, expectedYAML, runAppForTest(t, []string{"get", "config", "--include-server-config"}))
		assert.YAMLEq(t, expectedYAML, runAppForTest(t, []string{"get", "config", "--include-server-config", "--yaml"}))
		require.JSONEq(t, expectedJSON, runAppForTest(t, []string{"get", "config", "--include-server-config", "--json"}))
	})
}

func TestGetSoftware(t *testing.T) {
	_, ds := runServerWithMockedDS(t)

	foo001 := fleet.Software{
		Name: "foo", Version: "0.0.1", Source: "chrome_extensions", GenerateCPE: "somecpe",
		Vulnerabilities: fleet.Vulnerabilities{
			{CVE: "cve-321-432-543", DetailsLink: "https://nvd.nist.gov/vuln/detail/cve-321-432-543"},
			{CVE: "cve-333-444-555", DetailsLink: "https://nvd.nist.gov/vuln/detail/cve-333-444-555"},
		},
	}
	foo002 := fleet.Software{Name: "foo", Version: "0.0.2", Source: "chrome_extensions"}
	foo003 := fleet.Software{Name: "foo", Version: "0.0.3", Source: "chrome_extensions", GenerateCPE: "someothercpewithoutvulns"}
	bar003 := fleet.Software{Name: "bar", Version: "0.0.3", Source: "deb_packages", BundleIdentifier: "bundle"}

	var gotTeamID *uint

	ds.ListSoftwareFunc = func(ctx context.Context, opt fleet.SoftwareListOptions) ([]fleet.Software, error) {
		gotTeamID = opt.TeamID
		return []fleet.Software{foo001, foo002, foo003, bar003}, nil
	}

	expected := `+------+---------+-------------------+--------------------------+-----------+
| NAME | VERSION |      SOURCE       |           CPE            | # OF CVES |
+------+---------+-------------------+--------------------------+-----------+
| foo  | 0.0.1   | chrome_extensions | somecpe                  |         2 |
+------+---------+-------------------+--------------------------+-----------+
| foo  | 0.0.2   | chrome_extensions |                          |         0 |
+------+---------+-------------------+--------------------------+-----------+
| foo  | 0.0.3   | chrome_extensions | someothercpewithoutvulns |         0 |
+------+---------+-------------------+--------------------------+-----------+
| bar  | 0.0.3   | deb_packages      |                          |         0 |
+------+---------+-------------------+--------------------------+-----------+
`

	expectedYaml := `---
apiVersion: "1"
kind: software
spec:
- generated_cpe: somecpe
  id: 0
  name: foo
  source: chrome_extensions
  version: 0.0.1
  vulnerabilities:
  - cve: cve-321-432-543
    details_link: https://nvd.nist.gov/vuln/detail/cve-321-432-543
  - cve: cve-333-444-555
    details_link: https://nvd.nist.gov/vuln/detail/cve-333-444-555
- generated_cpe: ""
  id: 0
  name: foo
  source: chrome_extensions
  version: 0.0.2
  vulnerabilities: null
- generated_cpe: someothercpewithoutvulns
  id: 0
  name: foo
  source: chrome_extensions
  version: 0.0.3
  vulnerabilities: null
- bundle_identifier: bundle
  generated_cpe: ""
  id: 0
  name: bar
  source: deb_packages
  version: 0.0.3
  vulnerabilities: null
`

	expectedJson := `
{
  "kind": "software",
  "apiVersion": "1",
  "spec": [
    {
      "id": 0,
      "name": "foo",
      "version": "0.0.1",
      "source": "chrome_extensions",
      "generated_cpe": "somecpe",
      "vulnerabilities": [
        {
          "cve": "cve-321-432-543",
          "details_link": "https://nvd.nist.gov/vuln/detail/cve-321-432-543"
        },
        {
          "cve": "cve-333-444-555",
          "details_link": "https://nvd.nist.gov/vuln/detail/cve-333-444-555"
        }
      ]
    },
    {
      "id": 0,
      "name": "foo",
      "version": "0.0.2",
      "source": "chrome_extensions",
      "generated_cpe": "",
      "vulnerabilities": null
    },
    {
      "id": 0,
      "name": "foo",
      "version": "0.0.3",
      "source": "chrome_extensions",
      "generated_cpe": "someothercpewithoutvulns",
      "vulnerabilities": null
    },
    {
      "id": 0,
      "name": "bar",
      "version": "0.0.3",
      "bundle_identifier": "bundle",
      "source": "deb_packages",
      "generated_cpe": "",
      "vulnerabilities": null
    }
  ]
}
`

	assert.Equal(t, expected, runAppForTest(t, []string{"get", "software"}))
	assert.YAMLEq(t, expectedYaml, runAppForTest(t, []string{"get", "software", "--yaml"}))
	assert.JSONEq(t, expectedJson, runAppForTest(t, []string{"get", "software", "--json"}))

	runAppForTest(t, []string{"get", "software", "--json", "--team", "999"})
	require.NotNil(t, gotTeamID)
	assert.Equal(t, uint(999), *gotTeamID)
}

func TestGetLabels(t *testing.T) {
	_, ds := runServerWithMockedDS(t)

	ds.GetLabelSpecsFunc = func(ctx context.Context) ([]*fleet.LabelSpec, error) {
		return []*fleet.LabelSpec{
			{
				ID:          32,
				Name:        "label1",
				Description: "some description",
				Query:       "select 1;",
				Platform:    "windows",
			},
			{
				ID:          33,
				Name:        "label2",
				Description: "some other description",
				Query:       "select 42;",
				Platform:    "linux",
			},
		}, nil
	}

	expected := `+--------+----------+------------------------+------------+
|  NAME  | PLATFORM |      DESCRIPTION       |   QUERY    |
+--------+----------+------------------------+------------+
| label1 | windows  | some description       | select 1;  |
+--------+----------+------------------------+------------+
| label2 | linux    | some other description | select 42; |
+--------+----------+------------------------+------------+
`
	expectedYaml := `---
apiVersion: v1
kind: label
spec:
  description: some description
  id: 32
  label_membership_type: dynamic
  name: label1
  platform: windows
  query: select 1;
---
apiVersion: v1
kind: label
spec:
  description: some other description
  id: 33
  label_membership_type: dynamic
  name: label2
  platform: linux
  query: select 42;
`
	expectedJson := `{"kind":"label","apiVersion":"v1","spec":{"id":32,"name":"label1","description":"some description","query":"select 1;","platform":"windows","label_membership_type":"dynamic"}}
{"kind":"label","apiVersion":"v1","spec":{"id":33,"name":"label2","description":"some other description","query":"select 42;","platform":"linux","label_membership_type":"dynamic"}}
`

	assert.Equal(t, expected, runAppForTest(t, []string{"get", "labels"}))
	assert.Equal(t, expectedYaml, runAppForTest(t, []string{"get", "labels", "--yaml"}))
	assert.Equal(t, expectedJson, runAppForTest(t, []string{"get", "labels", "--json"}))
}

func TestGetLabel(t *testing.T) {
	_, ds := runServerWithMockedDS(t)

	ds.GetLabelSpecFunc = func(ctx context.Context, name string) (*fleet.LabelSpec, error) {
		if name != "label1" {
			return nil, nil
		}
		return &fleet.LabelSpec{
			ID:          32,
			Name:        "label1",
			Description: "some description",
			Query:       "select 1;",
			Platform:    "windows",
		}, nil
	}

	expectedYaml := `---
apiVersion: v1
kind: label
spec:
  description: some description
  id: 32
  label_membership_type: dynamic
  name: label1
  platform: windows
  query: select 1;
`
	expectedJson := `{"kind":"label","apiVersion":"v1","spec":{"id":32,"name":"label1","description":"some description","query":"select 1;","platform":"windows","label_membership_type":"dynamic"}}
`

	assert.Equal(t, expectedYaml, runAppForTest(t, []string{"get", "label", "label1"}))
	assert.Equal(t, expectedYaml, runAppForTest(t, []string{"get", "label", "--yaml", "label1"}))
	assert.Equal(t, expectedJson, runAppForTest(t, []string{"get", "label", "--json", "label1"}))
}

func TestGetEnrollmentSecrets(t *testing.T) {
	_, ds := runServerWithMockedDS(t)

	ds.GetEnrollSecretsFunc = func(ctx context.Context, teamID *uint) ([]*fleet.EnrollSecret, error) {
		return []*fleet.EnrollSecret{
			{
				Secret: "abcd",
				TeamID: nil,
			},
			{
				Secret: "efgh",
				TeamID: nil,
			},
		}, nil
	}

	expectedYaml := `---
apiVersion: v1
kind: enroll_secret
spec:
  secrets:
  - created_at: "0001-01-01T00:00:00Z"
    secret: abcd
  - created_at: "0001-01-01T00:00:00Z"
    secret: efgh
`
	expectedJson := `{"kind":"enroll_secret","apiVersion":"v1","spec":{"secrets":[{"secret":"abcd","created_at":"0001-01-01T00:00:00Z"},{"secret":"efgh","created_at":"0001-01-01T00:00:00Z"}]}}
`

	assert.Equal(t, expectedYaml, runAppForTest(t, []string{"get", "enroll_secrets"}))
	assert.Equal(t, expectedYaml, runAppForTest(t, []string{"get", "enroll_secrets", "--yaml"}))
	assert.Equal(t, expectedJson, runAppForTest(t, []string{"get", "enroll_secrets", "--json"}))
}

func TestGetPacks(t *testing.T) {
	_, ds := runServerWithMockedDS(t)

	ds.GetPackSpecsFunc = func(ctx context.Context) ([]*fleet.PackSpec, error) {
		return []*fleet.PackSpec{
			{
				ID:          7,
				Name:        "pack1",
				Description: "some desc",
				Platform:    "darwin",
				Disabled:    false,
			},
		}, nil
	}

	expected := `+-------+----------+-------------+----------+
| NAME  | PLATFORM | DESCRIPTION | DISABLED |
+-------+----------+-------------+----------+
| pack1 | darwin   | some desc   | false    |
+-------+----------+-------------+----------+
`
	expectedYaml := `---
apiVersion: v1
kind: pack
spec:
  description: some desc
  disabled: false
  id: 7
  name: pack1
  platform: darwin
  targets:
    labels: null
    teams: null
`
	expectedJson := `
{
  "kind": "pack",
  "apiVersion": "v1",
  "spec": {
    "id": 7,
    "name": "pack1",
    "description": "some desc",
    "platform": "darwin",
    "disabled": false,
    "targets": {
      "labels": null,
      "teams": null
    }
  }
}
`

	assert.Equal(t, expected, runAppForTest(t, []string{"get", "packs"}))
	assert.YAMLEq(t, expectedYaml, runAppForTest(t, []string{"get", "packs", "--yaml"}))
	assert.JSONEq(t, expectedJson, runAppForTest(t, []string{"get", "packs", "--json"}))
}

func TestGetPack(t *testing.T) {
	_, ds := runServerWithMockedDS(t)

	ds.PackByNameFunc = func(ctx context.Context, name string, opts ...fleet.OptionalArg) (*fleet.Pack, bool, error) {
		if name != "pack1" {
			return nil, false, nil
		}
		return &fleet.Pack{
			ID:          7,
			Name:        "pack1",
			Description: "some desc",
			Platform:    "darwin",
			Disabled:    false,
		}, true, nil
	}
	ds.GetPackSpecFunc = func(ctx context.Context, name string) (*fleet.PackSpec, error) {
		if name != "pack1" {
			return nil, nil
		}
		return &fleet.PackSpec{
			ID:          7,
			Name:        "pack1",
			Description: "some desc",
			Platform:    "darwin",
			Disabled:    false,
		}, nil
	}

	expectedYaml := `---
apiVersion: v1
kind: pack
spec:
  description: some desc
  disabled: false
  id: 7
  name: pack1
  platform: darwin
  targets:
    labels: null
    teams: null
`
	expectedJson := `
{
  "kind": "pack",
  "apiVersion": "v1",
  "spec": {
    "id": 7,
    "name": "pack1",
    "description": "some desc",
    "platform": "darwin",
    "disabled": false,
    "targets": {
      "labels": null,
      "teams": null
    }
  }
}
`

	assert.YAMLEq(t, expectedYaml, runAppForTest(t, []string{"get", "packs", "pack1"}))
	assert.YAMLEq(t, expectedYaml, runAppForTest(t, []string{"get", "packs", "--yaml", "pack1"}))
	assert.JSONEq(t, expectedJson, runAppForTest(t, []string{"get", "packs", "--json", "pack1"}))
}

func TestGetQueries(t *testing.T) {
	_, ds := runServerWithMockedDS(t)

	ds.ListQueriesFunc = func(ctx context.Context, opt fleet.ListQueryOptions) ([]*fleet.Query, error) {
		return []*fleet.Query{
			{
				ID:             33,
				Name:           "query1",
				Description:    "some desc",
				Query:          "select 1;",
				Saved:          false,
				ObserverCanRun: false,
			},
			{
				ID:             12,
				Name:           "query2",
				Description:    "some desc 2",
				Query:          "select 2;",
				Saved:          true,
				ObserverCanRun: false,
			},
		}, nil
	}

	expected := `+--------+-------------+-----------+
|  NAME  | DESCRIPTION |   QUERY   |
+--------+-------------+-----------+
| query1 | some desc   | select 1; |
+--------+-------------+-----------+
| query2 | some desc 2 | select 2; |
+--------+-------------+-----------+
`
	expectedYaml := `---
apiVersion: v1
kind: query
spec:
  description: some desc
  name: query1
  query: select 1;
---
apiVersion: v1
kind: query
spec:
  description: some desc 2
  name: query2
  query: select 2;
`
	expectedJson := `{"kind":"query","apiVersion":"v1","spec":{"name":"query1","description":"some desc","query":"select 1;"}}
{"kind":"query","apiVersion":"v1","spec":{"name":"query2","description":"some desc 2","query":"select 2;"}}
`

	assert.Equal(t, expected, runAppForTest(t, []string{"get", "queries"}))
	assert.Equal(t, expectedYaml, runAppForTest(t, []string{"get", "queries", "--yaml"}))
	assert.Equal(t, expectedJson, runAppForTest(t, []string{"get", "queries", "--json"}))
}

func TestGetQuery(t *testing.T) {
	_, ds := runServerWithMockedDS(t)

	ds.QueryByNameFunc = func(ctx context.Context, name string, opts ...fleet.OptionalArg) (*fleet.Query, error) {
		if name != "query1" {
			return nil, nil
		}
		return &fleet.Query{
			ID:             33,
			Name:           "query1",
			Description:    "some desc",
			Query:          "select 1;",
			Saved:          false,
			ObserverCanRun: false,
		}, nil
	}

	expectedYaml := `---
apiVersion: v1
kind: query
spec:
  description: some desc
  name: query1
  query: select 1;
`
	expectedJson := `{"kind":"query","apiVersion":"v1","spec":{"name":"query1","description":"some desc","query":"select 1;"}}
`

	assert.Equal(t, expectedYaml, runAppForTest(t, []string{"get", "query", "query1"}))
	assert.Equal(t, expectedYaml, runAppForTest(t, []string{"get", "query", "--yaml", "query1"}))
	assert.Equal(t, expectedJson, runAppForTest(t, []string{"get", "query", "--json", "query1"}))
}

// TestGetQueriesAsObservers tests that when observers run `fleectl get queries` they
// only get queries that they can execute.
func TestGetQueriesAsObserver(t *testing.T) {
	_, ds := runServerWithMockedDS(t)

	setCurrentUserSession := func(user *fleet.User) {
		user, err := ds.NewUser(context.Background(), user)
		require.NoError(t, err)
		ds.SessionByKeyFunc = func(ctx context.Context, key string) (*fleet.Session, error) {
			return &fleet.Session{
				CreateTimestamp: fleet.CreateTimestamp{CreatedAt: time.Now()},
				ID:              1,
				AccessedAt:      time.Now(),
				UserID:          user.ID,
				Key:             key,
			}, nil
		}
		ds.UserByIDFunc = func(ctx context.Context, id uint) (*fleet.User, error) {
			return user, nil
		}
	}

	ds.ListQueriesFunc = func(ctx context.Context, opt fleet.ListQueryOptions) ([]*fleet.Query, error) {
		return []*fleet.Query{
			{
				ID:             42,
				Name:           "query1",
				Description:    "some desc",
				Query:          "select 1;",
				ObserverCanRun: false,
			},
			{
				ID:             43,
				Name:           "query2",
				Description:    "some desc 2",
				Query:          "select 2;",
				ObserverCanRun: true,
			},
			{
				ID:             44,
				Name:           "query3",
				Description:    "some desc 3",
				Query:          "select 3;",
				ObserverCanRun: false,
			},
		}, nil
	}

	for _, tc := range []struct {
		name string
		user *fleet.User
	}{
		{
			name: "global observer",
			user: &fleet.User{
				ID:         1,
				Name:       "Global observer",
				Password:   []byte("p4ssw0rd.123"),
				Email:      "go@example.com",
				GlobalRole: ptr.String(fleet.RoleObserverPlus),
			},
		},
		{
			name: "team observer",
			user: &fleet.User{
				ID:         2,
				Name:       "Team observer",
				Password:   []byte("p4ssw0rd.123"),
				Email:      "tm@example.com",
				GlobalRole: nil,
				Teams:      []fleet.UserTeam{{Role: fleet.RoleObserver}},
			},
		},
		{
			name: "observer of multiple teams",
			user: &fleet.User{
				ID:         3,
				Name:       "Observer of multiple teams",
				Password:   []byte("p4ssw0rd.123"),
				Email:      "omt@example.com",
				GlobalRole: nil,
				Teams: []fleet.UserTeam{
					{
						Team: fleet.Team{ID: 1},
						Role: fleet.RoleObserver,
					},
					{
						Team: fleet.Team{ID: 2},
						Role: fleet.RoleObserverPlus,
					},
				},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setCurrentUserSession(tc.user)

			expected := `+--------+-------------+-----------+
|  NAME  | DESCRIPTION |   QUERY   |
+--------+-------------+-----------+
| query2 | some desc 2 | select 2; |
+--------+-------------+-----------+
`
			expectedYaml := `---
apiVersion: v1
kind: query
spec:
  description: some desc 2
  name: query2
  query: select 2;
`
			expectedJson := `{"kind":"query","apiVersion":"v1","spec":{"name":"query2","description":"some desc 2","query":"select 2;"}}
`

			assert.Equal(t, expected, runAppForTest(t, []string{"get", "queries"}))
			assert.Equal(t, expectedYaml, runAppForTest(t, []string{"get", "queries", "--yaml"}))
			assert.Equal(t, expectedJson, runAppForTest(t, []string{"get", "queries", "--json"}))
		})
	}

	// Test with a user that is observer of a team, but maintainer of another team (should not filter the queries).
	setCurrentUserSession(&fleet.User{
		ID:         4,
		Name:       "Not observer of all teams",
		Password:   []byte("p4ssw0rd.123"),
		Email:      "omt2@example.com",
		GlobalRole: nil,
		Teams: []fleet.UserTeam{
			{
				Team: fleet.Team{ID: 1},
				Role: fleet.RoleObserver,
			},
			{
				Team: fleet.Team{ID: 2},
				Role: fleet.RoleMaintainer,
			},
		},
	})

	expected := `+--------+-------------+-----------+
|  NAME  | DESCRIPTION |   QUERY   |
+--------+-------------+-----------+
| query1 | some desc   | select 1; |
+--------+-------------+-----------+
| query2 | some desc 2 | select 2; |
+--------+-------------+-----------+
| query3 | some desc 3 | select 3; |
+--------+-------------+-----------+
`
	expectedYaml := `---
apiVersion: v1
kind: query
spec:
  description: some desc
  name: query1
  query: select 1;
---
apiVersion: v1
kind: query
spec:
  description: some desc 2
  name: query2
  query: select 2;
---
apiVersion: v1
kind: query
spec:
  description: some desc 3
  name: query3
  query: select 3;
`
	expectedJson := `{"kind":"query","apiVersion":"v1","spec":{"name":"query1","description":"some desc","query":"select 1;"}}
{"kind":"query","apiVersion":"v1","spec":{"name":"query2","description":"some desc 2","query":"select 2;"}}
{"kind":"query","apiVersion":"v1","spec":{"name":"query3","description":"some desc 3","query":"select 3;"}}
`

	assert.Equal(t, expected, runAppForTest(t, []string{"get", "queries"}))
	assert.Equal(t, expectedYaml, runAppForTest(t, []string{"get", "queries", "--yaml"}))
	assert.Equal(t, expectedJson, runAppForTest(t, []string{"get", "queries", "--json"}))

	// No queries are returned if none is observer_can_run.
	setCurrentUserSession(&fleet.User{
		ID:         2,
		Name:       "Team observer",
		Password:   []byte("p4ssw0rd.123"),
		Email:      "tm@example.com",
		GlobalRole: nil,
		Teams:      []fleet.UserTeam{{Role: fleet.RoleObserver}},
	})
	ds.ListQueriesFunc = func(ctx context.Context, opt fleet.ListQueryOptions) ([]*fleet.Query, error) {
		return []*fleet.Query{
			{
				ID:             42,
				Name:           "query1",
				Description:    "some desc",
				Query:          "select 1;",
				ObserverCanRun: false,
			},
			{
				ID:             43,
				Name:           "query2",
				Description:    "some desc 2",
				Query:          "select 2;",
				ObserverCanRun: false,
			},
		}, nil
	}
	assert.Equal(t, "", runAppForTest(t, []string{"get", "queries"}))

	// No filtering is performed if all are observer_can_run.
	ds.ListQueriesFunc = func(ctx context.Context, opt fleet.ListQueryOptions) ([]*fleet.Query, error) {
		return []*fleet.Query{
			{
				ID:             42,
				Name:           "query1",
				Description:    "some desc",
				Query:          "select 1;",
				ObserverCanRun: true,
			},
			{
				ID:             43,
				Name:           "query2",
				Description:    "some desc 2",
				Query:          "select 2;",
				ObserverCanRun: true,
			},
		}, nil
	}
	expected = `+--------+-------------+-----------+
|  NAME  | DESCRIPTION |   QUERY   |
+--------+-------------+-----------+
| query1 | some desc   | select 1; |
+--------+-------------+-----------+
| query2 | some desc 2 | select 2; |
+--------+-------------+-----------+
`
	assert.Equal(t, expected, runAppForTest(t, []string{"get", "queries"}))
}

func TestEnrichedAppConfig(t *testing.T) {
	t.Run("deprecated fields", func(t *testing.T) {
		resp := []byte(`
      {
        "org_info": {
          "org_name": "Fleet for osquery",
          "org_logo_url": ""
        },
        "server_settings": {
          "server_url": "https://localhost:8412",
          "live_query_disabled": false,
          "enable_analytics": false,
          "deferred_save_host": false
        },
        "smtp_settings": {
          "enable_smtp": false,
          "configured": false,
          "sender_address": "",
          "server": "",
          "port": 587,
          "authentication_type": "authtype_username_password",
          "user_name": "",
          "password": "",
          "enable_ssl_tls": true,
          "authentication_method": "authmethod_plain",
          "domain": "",
          "verify_ssl_certs": true,
          "enable_start_tls": true
        },
        "host_expiry_settings": {
          "host_expiry_enabled": false,
          "host_expiry_window": 0
        },
        "host_settings": {
          "enable_host_users": true,
          "enable_software_inventory": true
        },
        "agent_options": {
          "config": {
            "options": {
              "logger_plugin": "tls",
              "pack_delimiter": "/",
              "logger_tls_period": 10,
              "distributed_plugin": "tls",
              "disable_distributed": false,
              "logger_tls_endpoint": "/api/osquery/log",
              "distributed_interval": 10,
              "distributed_tls_max_attempts": 3
            },
            "decorators": {
              "load": [
                "SELECT uuid AS host_uuid FROM system_info;",
                "SELECT hostname AS hostname FROM system_info;"
              ]
            }
          },
          "overrides": {}
        },
        "sso_settings": {
          "entity_id": "",
          "issuer_uri": "",
          "idp_image_url": "",
          "metadata": "",
          "metadata_url": "",
          "idp_name": "",
          "enable_sso": false,
          "enable_sso_idp_login": false,
          "enable_jit_provisioning": false,
          "enable_jit_role_sync": false
        },
        "fleet_desktop": {
          "transparency_url": "https://fleetdm.com/transparency"
        },
        "vulnerability_settings": {
          "databases_path": ""
        },
        "webhook_settings": {
          "host_status_webhook": {
            "enable_host_status_webhook": false,
            "destination_url": "",
            "host_percentage": 0,
            "days_count": 0
          },
          "failing_policies_webhook": {
            "enable_failing_policies_webhook": false,
            "destination_url": "",
            "policy_ids": null,
            "host_batch_size": 0
          },
          "vulnerabilities_webhook": {
            "enable_vulnerabilities_webhook": false,
            "destination_url": "",
            "host_batch_size": 0
          },
          "interval": "24h0m0s"
        },
        "integrations": {
          "jira": null,
          "zendesk": null
        },
        "update_interval": {
          "osquery_detail": 3600000000000,
          "osquery_policy": 3600000000000
        },
        "vulnerabilities": {
          "databases_path": "/vulndb",
          "periodicity": 300000000000,
          "cpe_database_url": "",
          "cve_feed_prefix_url": "",
          "current_instance_checks": "yes",
          "disable_data_sync": false,
          "recent_vulnerability_max_age": 2592000000000000
        },
        "license": {
          "tier": "free",
          "expiration": "0001-01-01T00:00:00Z"
        },
        "logging": {
          "debug": true,
          "json": true,
          "result": {
            "plugin": "filesystem",
            "config": {
              "status_log_file": "/logs/osqueryd.status.log",
              "result_log_file": "/logs/osqueryd.results.log",
              "enable_log_rotation": false,
              "enable_log_compression": false
            }
          },
          "status": {
            "plugin": "filesystem",
            "config": {
              "status_log_file": "/logs/osqueryd.status.log",
              "result_log_file": "/logs/osqueryd.results.log",
              "enable_log_rotation": false,
              "enable_log_compression": false
            }
          }
        }
      }
    `)

		var enriched fleet.EnrichedAppConfig
		err := json.Unmarshal(resp, &enriched)
		require.NoError(t, err)
		require.NotNil(t, enriched.Vulnerabilities)
		require.Equal(t, "yes", enriched.Vulnerabilities.CurrentInstanceChecks)
		require.True(t, enriched.Features.EnableSoftwareInventory)
		require.Equal(t, "free", enriched.License.Tier)
		require.Equal(t, "filesystem", enriched.Logging.Status.Plugin)
	})
}

func TestGetAppleMDM(t *testing.T) {
	_, ds := runServerWithMockedDS(t)

	ds.AppConfigFunc = func(ctx context.Context) (*fleet.AppConfig, error) {
		return &fleet.AppConfig{MDM: fleet.MDM{EnabledAndConfigured: true}}, nil
	}

	// can only test when no MDM cert is provided, otherwise they would have to
	// be valid Apple APNs and SCEP certs.
	expected := `Error: No Apple Push Notification service (APNs) certificate found.`
	assert.Contains(t, runAppForTest(t, []string{"get", "mdm_apple"}), expected)
}

func TestGetAppleBM(t *testing.T) {
	t.Run("free license", func(t *testing.T) {
		runServerWithMockedDS(t)

		expected := `could not get Apple BM information: missing or invalid license`
		_, err := runAppNoChecks([]string{"get", "mdm_apple_bm"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), expected)
	})

	t.Run("premium license", func(t *testing.T) {
		runServerWithMockedDS(t, &service.TestServerOpts{License: &fleet.LicenseInfo{Tier: fleet.TierPremium}})

		expected := `No Apple Business Manager server token found`
		assert.Contains(t, runAppForTest(t, []string{"get", "mdm_apple_bm"}), expected)
	})
}

func TestGetCarves(t *testing.T) {
	_, ds := runServerWithMockedDS(t)

	createdAt, err := time.Parse(time.RFC3339, "1999-03-10T02:45:06.371Z")
	require.NoError(t, err)
	ds.ListCarvesFunc = func(ctx context.Context, opts fleet.CarveListOptions) ([]*fleet.CarveMetadata, error) {
		return []*fleet.CarveMetadata{
			{
				HostId:     1,
				Name:       "foobar",
				BlockCount: 10,
				BlockSize:  12,
				CarveSize:  123,
				CarveId:    "carve_id_1",
				RequestId:  "request_id_1",
				SessionId:  "session_id_1",
				CreatedAt:  createdAt,
			},
			{
				HostId:     2,
				Name:       "barfoo",
				BlockCount: 20,
				BlockSize:  44,
				CarveSize:  123,
				CarveId:    "carve_id_2",
				RequestId:  "request_id_2",
				SessionId:  "session_id_2",
				CreatedAt:  createdAt,
				Error:      ptr.String("test error"),
			},
		}, nil
	}

	expected := `+----+--------------------------------+--------------+------------+------------+---------+
| ID |           CREATED AT           |  REQUEST ID  | CARVE SIZE | COMPLETION | ERRORED |
+----+--------------------------------+--------------+------------+------------+---------+
|  0 | 1999-03-10 02:45:06.371 +0000  | request_id_1 |        123 | 10%        | no      |
|    | UTC                            |              |            |            |         |
+----+--------------------------------+--------------+------------+------------+---------+
|  0 | 1999-03-10 02:45:06.371 +0000  | request_id_2 |        123 | 5%         | yes     |
|    | UTC                            |              |            |            |         |
+----+--------------------------------+--------------+------------+------------+---------+
`
	assert.Equal(t, expected, runAppForTest(t, []string{"get", "carves"}))
}

func TestGetCarve(t *testing.T) {
	_, ds := runServerWithMockedDS(t)

	createdAt, err := time.Parse(time.RFC3339, "1999-03-10T02:45:06.371Z")
	require.NoError(t, err)
	ds.CarveFunc = func(ctx context.Context, carveID int64) (*fleet.CarveMetadata, error) {
		return &fleet.CarveMetadata{
			HostId:     1,
			Name:       "foobar",
			BlockCount: 10,
			BlockSize:  12,
			CarveSize:  123,
			CarveId:    "carve_id_1",
			RequestId:  "request_id_1",
			SessionId:  "session_id_1",
			CreatedAt:  createdAt,
		}, nil
	}

	expectedOut := `---
block_count: 10
block_size: 12
carve_id: carve_id_1
carve_size: 123
created_at: "1999-03-10T02:45:06.371Z"
error: null
expired: false
host_id: 1
id: 0
max_block: 0
name: foobar
request_id: request_id_1
session_id: session_id_1
`

	assert.Equal(t, expectedOut, runAppForTest(t, []string{"get", "carve", "1"}))
}

func TestGetCarveWithError(t *testing.T) {
	_, ds := runServerWithMockedDS(t)

	createdAt, err := time.Parse(time.RFC3339, "1999-03-10T02:45:06.371Z")
	require.NoError(t, err)
	ds.CarveFunc = func(ctx context.Context, carveID int64) (*fleet.CarveMetadata, error) {
		return &fleet.CarveMetadata{
			HostId:     1,
			Name:       "foobar",
			BlockCount: 10,
			BlockSize:  12,
			CarveSize:  123,
			CarveId:    "carve_id_1",
			RequestId:  "request_id_1",
			SessionId:  "session_id_1",
			CreatedAt:  createdAt,
			Error:      ptr.String("test error"),
		}, nil
	}

	runAppCheckErr(t, []string{"get", "carve", "1"}, "test error")
}

// TestGetTeamsYAMLAndApply checks that the output of `get teams --yaml` can be applied
// via the `apply` command.
func TestGetTeamsYAMLAndApply(t *testing.T) {
	cfg := config.TestConfig()
	_, ds := runServerWithMockedDS(t, &service.TestServerOpts{
		License:     &fleet.LicenseInfo{Tier: fleet.TierPremium, Expiration: time.Now().Add(24 * time.Hour)},
		FleetConfig: &cfg,
	})

	created_at, err := time.Parse(time.RFC3339, "1999-03-10T02:45:06.371Z")
	require.NoError(t, err)
	agentOpts := json.RawMessage(`
{
  "config": {
      "options": {
        "distributed_interval": 10
      }
  },
  "overrides": {
    "platforms": {
      "darwin": {
        "options": {
          "distributed_interval": 5
        }
      }
    }
  }
}`)
	additionalQueries := json.RawMessage(`{"time":"SELECT * FROM time;"}`)
	team1 := &fleet.Team{
		ID:          42,
		CreatedAt:   created_at,
		Name:        "team1",
		Description: "team1 description",
		UserCount:   99,
		Config: fleet.TeamConfig{
			Features: fleet.Features{
				EnableHostUsers:         true,
				EnableSoftwareInventory: true,
			},
		},
	}
	team2 := &fleet.Team{
		ID:          43,
		CreatedAt:   created_at,
		Name:        "team2",
		Description: "team2 description",
		UserCount:   87,
		Config: fleet.TeamConfig{
			AgentOptions: &agentOpts,
			Features: fleet.Features{
				AdditionalQueries: &additionalQueries,
			},
			MDM: fleet.TeamMDM{
				MacOSUpdates: fleet.MacOSUpdates{
					MinimumVersion: "12.3.1",
					Deadline:       "2021-12-14",
				},
			},
		},
	}
	ds.ListTeamsFunc = func(ctx context.Context, filter fleet.TeamFilter, opt fleet.ListOptions) ([]*fleet.Team, error) {
		return []*fleet.Team{team1, team2}, nil
	}
	ds.AppConfigFunc = func(ctx context.Context) (*fleet.AppConfig, error) {
		return &fleet.AppConfig{AgentOptions: &agentOpts, MDM: fleet.MDM{EnabledAndConfigured: true}}, nil
	}
	ds.SaveTeamFunc = func(ctx context.Context, team *fleet.Team) (*fleet.Team, error) {
		return team, nil
	}
	ds.ApplyEnrollSecretsFunc = func(ctx context.Context, teamID *uint, secrets []*fleet.EnrollSecret) error {
		return nil
	}
	ds.NewActivityFunc = func(ctx context.Context, user *fleet.User, activity fleet.ActivityDetails) error {
		return nil
	}
	ds.TeamByNameFunc = func(ctx context.Context, name string) (*fleet.Team, error) {
		if name == "team1" {
			return team1, nil
		} else if name == "team2" {
			return team2, nil
		}
		return nil, fmt.Errorf("team not found: %s", name)
	}
	ds.BatchSetMDMAppleProfilesFunc = func(ctx context.Context, teamID *uint, profiles []*fleet.MDMAppleConfigProfile) error {
		return nil
	}
	ds.BulkSetPendingMDMAppleHostProfilesFunc = func(ctx context.Context, hostIDs, teamIDs, profileIDs []uint, uuids []string) error {
		return nil
	}

	actualYaml := runAppForTest(t, []string{"get", "teams", "--yaml"})
	yamlFilePath := writeTmpYml(t, actualYaml)

	require.Equal(t, "[+] applied 2 teams\n", runAppForTest(t, []string{"apply", "-f", yamlFilePath}))
}

func TestGetMDMCommandResults(t *testing.T) {
	_, ds := runServerWithMockedDS(t)

	rawXml := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Command</key>
    <dict>
        <key>ManagedOnly</key>
        <false/>
        <key>RequestType</key>
        <string>ProfileList</string>
    </dict>
    <key>CommandUUID</key>
    <string>0001_ProfileList</string>
</dict>
</plist>`

	ds.AppConfigFunc = func(ctx context.Context) (*fleet.AppConfig, error) {
		return &fleet.AppConfig{MDM: fleet.MDM{EnabledAndConfigured: true}}, nil
	}
	ds.ListHostsLiteByUUIDsFunc = func(ctx context.Context, filter fleet.TeamFilter, uuids []string) ([]*fleet.Host, error) {
		if len(uuids) == 0 {
			return nil, nil
		}
		require.Len(t, uuids, 2)
		return []*fleet.Host{
			{ID: 1, UUID: uuids[0], Hostname: "host1"},
			{ID: 2, UUID: uuids[1], Hostname: "host2"},
		}, nil
	}
	ds.GetMDMAppleCommandRequestTypeFunc = func(ctx context.Context, commandUUID string) (string, error) {
		if commandUUID == "no-such-cmd" {
			return "", &notFoundError{}
		}
		return "test", nil
	}
	ds.GetMDMAppleCommandResultsFunc = func(ctx context.Context, commandUUID string) ([]*fleet.MDMAppleCommandResult, error) {
		switch commandUUID {
		case "empty-cmd":
			return nil, nil
		case "fail-cmd":
			return nil, io.EOF
		default:
			return []*fleet.MDMAppleCommandResult{
				{
					DeviceID:    "device1",
					CommandUUID: commandUUID,
					Status:      "Acknowledged",
					UpdatedAt:   time.Date(2023, 4, 4, 15, 29, 0, 0, time.UTC),
					RequestType: "test",
					Result:      []byte(rawXml),
				},
				{
					DeviceID:    "device2",
					CommandUUID: commandUUID,
					Status:      "Error",
					UpdatedAt:   time.Date(2023, 4, 4, 15, 29, 0, 0, time.UTC),
					RequestType: "test",
					Result:      []byte(rawXml),
				},
			}, nil
		}
	}

	_, err := runAppNoChecks([]string{"get", "mdm-command-results"})
	require.Error(t, err)
	require.ErrorContains(t, err, `Required flag "id" not set`)

	_, err = runAppNoChecks([]string{"get", "mdm-command-results", "--id", "no-such-cmd"})
	require.Error(t, err)
	require.ErrorContains(t, err, `The command doesn't exist.`)

	_, err = runAppNoChecks([]string{"get", "mdm-command-results", "--id", "fail-cmd"})
	require.Error(t, err)
	require.ErrorContains(t, err, `EOF`)

	buf, err := runAppNoChecks([]string{"get", "mdm-command-results", "--id", "empty-cmd"})
	require.NoError(t, err)
	require.Contains(t, buf.String(), strings.TrimSpace(`
+----+------+------+--------+----------+---------+
| ID | TIME | TYPE | STATUS | HOSTNAME | RESULTS |
+----+------+------+--------+----------+---------+
`))

	buf, err = runAppNoChecks([]string{"get", "mdm-command-results", "--id", "valid-cmd"})
	require.NoError(t, err)
	require.Contains(t, buf.String(), strings.TrimSpace(`
+-----------+----------------------+------+--------------+----------+---------------------------------------------------+
|    ID     |         TIME         | TYPE |    STATUS    | HOSTNAME |                      RESULTS                      |
+-----------+----------------------+------+--------------+----------+---------------------------------------------------+
| valid-cmd | 2023-04-04T15:29:00Z | test | Acknowledged | host1    | <?xml version="1.0" encoding="UTF-8"?> <!DOCTYPE  |
|           |                      |      |              |          | plist PUBLIC "-//Apple//DTD PLIST 1.0//EN"        |
|           |                      |      |              |          | "http://www.apple.com/DTDs/PropertyList-1.0.dtd"> |
|           |                      |      |              |          | <plist version="1.0"> <dict>                      |
|           |                      |      |              |          | <key>Command</key>     <dict>                     |
|           |                      |      |              |          | <key>ManagedOnly</key>         <false/>           |
|           |                      |      |              |          |         <key>RequestType</key>                    |
|           |                      |      |              |          |    <string>ProfileList</string>                   |
|           |                      |      |              |          | </dict>     <key>CommandUUID</key>                |
|           |                      |      |              |          | <string>0001_ProfileList</string> </dict>         |
|           |                      |      |              |          | </plist>                                          |
+-----------+----------------------+------+--------------+----------+---------------------------------------------------+
| valid-cmd | 2023-04-04T15:29:00Z | test | Error        | host2    | <?xml version="1.0" encoding="UTF-8"?> <!DOCTYPE  |
|           |                      |      |              |          | plist PUBLIC "-//Apple//DTD PLIST 1.0//EN"        |
|           |                      |      |              |          | "http://www.apple.com/DTDs/PropertyList-1.0.dtd"> |
|           |                      |      |              |          | <plist version="1.0"> <dict>                      |
|           |                      |      |              |          | <key>Command</key>     <dict>                     |
|           |                      |      |              |          | <key>ManagedOnly</key>         <false/>           |
|           |                      |      |              |          |         <key>RequestType</key>                    |
|           |                      |      |              |          |    <string>ProfileList</string>                   |
|           |                      |      |              |          | </dict>     <key>CommandUUID</key>                |
|           |                      |      |              |          | <string>0001_ProfileList</string> </dict>         |
|           |                      |      |              |          | </plist>                                          |
+-----------+----------------------+------+--------------+----------+---------------------------------------------------+
`))
}

func TestGetMDMCommands(t *testing.T) {
	_, ds := runServerWithMockedDS(t)

	ds.AppConfigFunc = func(ctx context.Context) (*fleet.AppConfig, error) {
		return &fleet.AppConfig{MDM: fleet.MDM{EnabledAndConfigured: true}}, nil
	}
	var empty bool
	var listErr error
	ds.ListMDMAppleCommandsFunc = func(ctx context.Context, tmFilter fleet.TeamFilter, listOpts *fleet.MDMAppleCommandListOptions) ([]*fleet.MDMAppleCommand, error) {
		if empty || listErr != nil {
			return nil, listErr
		}
		return []*fleet.MDMAppleCommand{
			{
				DeviceID:    "h1",
				CommandUUID: "u1",
				UpdatedAt:   time.Date(2023, 4, 12, 9, 5, 0, 0, time.UTC),
				RequestType: "ProfileList",
				Status:      "Acknowledged",
				Hostname:    "host1",
			},
			{
				DeviceID:    "h2",
				CommandUUID: "u2",
				UpdatedAt:   time.Date(2023, 4, 11, 9, 5, 0, 0, time.UTC),
				RequestType: "ListApps",
				Status:      "Acknowledged",
				Hostname:    "host2",
			},
		}, nil
	}

	listErr = io.ErrUnexpectedEOF
	_, err := runAppNoChecks([]string{"get", "mdm-commands"})
	require.Error(t, err)
	require.ErrorContains(t, err, io.ErrUnexpectedEOF.Error())

	listErr = nil
	empty = true
	buf, err := runAppNoChecks([]string{"get", "mdm-commands"})
	require.NoError(t, err)
	require.Contains(t, buf.String(), "You haven't run any MDM commands. Run MDM commands with the `fleetctl mdm run-command` command.")

	empty = false
	buf, err = runAppNoChecks([]string{"get", "mdm-commands"})
	require.NoError(t, err)
	require.Contains(t, buf.String(), strings.TrimSpace(`
+----+----------------------+-------------+--------------+----------+
| ID |         TIME         |    TYPE     |    STATUS    | HOSTNAME |
+----+----------------------+-------------+--------------+----------+
| u1 | 2023-04-12T09:05:00Z | ProfileList | Acknowledged | host1    |
+----+----------------------+-------------+--------------+----------+
| u2 | 2023-04-11T09:05:00Z | ListApps    | Acknowledged | host2    |
+----+----------------------+-------------+--------------+----------+
`))
}

func TestUserIsObserver(t *testing.T) {
	for _, tc := range []struct {
		name        string
		user        fleet.User
		expectedVal bool
		expectedErr error
	}{
		{
			name:        "user without roles",
			user:        fleet.User{},
			expectedErr: errUserNoRoles,
		},
		{
			name:        "global observer",
			user:        fleet.User{GlobalRole: ptr.String(fleet.RoleObserver)},
			expectedVal: true,
		},
		{
			name:        "global observer+",
			user:        fleet.User{GlobalRole: ptr.String(fleet.RoleObserverPlus)},
			expectedVal: true,
		},
		{
			name:        "global maintainer",
			user:        fleet.User{GlobalRole: ptr.String(fleet.RoleMaintainer)},
			expectedVal: false,
		},
		{
			name: "team observer",
			user: fleet.User{
				GlobalRole: nil,
				Teams: []fleet.UserTeam{
					{Role: fleet.RoleObserver},
				},
			},
			expectedVal: true,
		},
		{
			name: "team observer+",
			user: fleet.User{
				GlobalRole: nil,
				Teams: []fleet.UserTeam{
					{Role: fleet.RoleObserverPlus},
				},
			},
			expectedVal: true,
		},
		{
			name: "team maintainer",
			user: fleet.User{
				GlobalRole: nil,
				Teams: []fleet.UserTeam{
					{Role: fleet.RoleMaintainer},
				},
			},
			expectedVal: false,
		},
		{
			name: "team observer and maintainer",
			user: fleet.User{
				GlobalRole: nil,
				Teams: []fleet.UserTeam{
					{Role: fleet.RoleObserver},
					{Role: fleet.RoleMaintainer},
				},
			},
			expectedVal: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			actual, err := userIsObserver(tc.user)
			require.Equal(t, tc.expectedErr, err)
			require.Equal(t, tc.expectedVal, actual)
		})
	}
}
