//go:build integration

//	Usage: ZITI_MGMT_URL=https://host:port/edge/management/v1 ZITI_USERNAME=... ZITI_PASSWORD=... \
//	  [ZITI_CA_FILE=ca.pem] go test -tags integration ./internal/ziti/
package ziti

import (
	"crypto/rand"
	"crypto/x509"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"testing"

	"github.com/openziti/edge-api/rest_management_api_client"
	"github.com/openziti/edge-api/rest_management_api_client/config"
	"github.com/openziti/edge-api/rest_model"
	"github.com/openziti/edge-api/rest_util"
)

func spikeClient(t *testing.T) *rest_management_api_client.ZitiEdgeManagement {
	mgmt := os.Getenv("ZITI_MGMT_URL")
	if mgmt == "" {
		t.Skip("ZITI_MGMT_URL not set")
	}
	var pool *x509.CertPool
	if f := os.Getenv("ZITI_CA_FILE"); f != "" {
		pem, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		pool = x509.NewCertPool()
		pool.AppendCertsFromPEM(pem)
	} else {
		u, _ := url.Parse(mgmt)
		var err error
		if pool, err = rest_util.GetControllerWellKnownCaPool("https://" + u.Host); err != nil {
			t.Fatal(err)
		}
	}
	c, err := rest_util.NewEdgeManagementClientWithUpdb(os.Getenv("ZITI_USERNAME"), os.Getenv("ZITI_PASSWORD"), mgmt, pool)
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	return c
}

func listConfigs(c *rest_management_api_client.ZitiEdgeManagement, filter string) ([]*rest_model.ConfigDetail, error) {
	p := config.NewListConfigsParams()
	p.Filter = &filter
	r, err := c.Config.ListConfigs(p, nil)
	if err != nil {
		return nil, err
	}
	return r.Payload.Data, nil
}

func TestSpikeTaggedConfigLifecycle(t *testing.T) {
	c := spikeClient(t)

	f := `name="intercept.v1"`
	ct, err := c.Config.ListConfigTypes(&config.ListConfigTypesParams{Filter: &f, Context: t.Context()}, nil)
	if err != nil || len(ct.Payload.Data) != 1 {
		t.Fatalf("intercept.v1 config type: %v", err)
	}

	b := make([]byte, 8)
	rand.Read(b)
	uid := hex.EncodeToString(b)
	name := "spike-" + uid + "-intercept.v1"
	created, err := c.Config.CreateConfig(&config.CreateConfigParams{Context: t.Context(), Config: &rest_model.ConfigCreate{
		Name:         &name,
		ConfigTypeID: ct.Payload.Data[0].ID,
		Data: map[string]any{
			"addresses":  []string{"spike.example.com"},
			"portRanges": []map[string]int{{"low": 443, "high": 443}},
			"protocols":  []string{"tcp"},
		},
		Tags: &rest_model.Tags{SubTags: rest_model.SubTags{
			"ziti-operator-uid":     uid,
			"ziti-operator-cluster": "spike",
		}},
	}}, nil)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	id := created.Payload.Data.ID
	t.Cleanup(func() { c.Config.DeleteConfig(&config.DeleteConfigParams{ID: id}, nil) })

	byUID := fmt.Sprintf(`tags.ziti-operator-uid="%s"`, uid)
	got, err := listConfigs(c, byUID)
	if err != nil || len(got) != 1 || *got[0].ID != id {
		t.Fatalf("filter by tag: got %d, err %v", len(got), err)
	}

	// Controller v2.0.4 rejects tag keys with digits, dots, or slashes in filters.
	if _, err := listConfigs(c, `tags.k8s.openziti.io/uid="x"`); err == nil {
		t.Error("dotted tag key filter now parses; flat keys may no longer be needed")
	}

	if _, err := c.Config.DeleteConfig(&config.DeleteConfigParams{ID: id, Context: t.Context()}, nil); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if got, err := listConfigs(c, byUID); err != nil || len(got) != 0 {
		t.Fatalf("after delete: got %d, err %v", len(got), err)
	}
}
