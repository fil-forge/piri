package receipts_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	cdm "github.com/fil-forge/libforge/capabilities/datamodel"
	"github.com/fil-forge/libforge/testutil"
	"github.com/fil-forge/ucantone/ipld/datamodel"
	"github.com/fil-forge/ucantone/ucan/container"
	"github.com/fil-forge/ucantone/ucan/invocation"
	"github.com/fil-forge/ucantone/ucan/receipt"
	"github.com/stretchr/testify/require"

	"github.com/fil-forge/piri/pkg/client/receipts"
)

func TestFetch(t *testing.T) {
	t.Run("found", func(t *testing.T) {
		alice := testutil.Alice
		service := testutil.RandomSigner(t)

		inv, err := invocation.Invoke(
			alice,
			alice.DID(),
			"/test/receipt",
			datamodel.Map{},
			invocation.WithAudience(service.DID()),
		)
		require.NoError(t, err)

		rcpt, err := receipt.IssueOK(service, inv.Link(), &cdm.UnitModel{})
		require.NoError(t, err)

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ct := container.New(container.WithReceipts(rcpt))
			b, err := container.Encode(container.Raw, ct)
			require.NoError(t, err)
			_, err = w.Write(b)
			require.NoError(t, err)
		}))
		defer server.Close()

		endpoint, err := url.Parse(server.URL)
		require.NoError(t, err)

		client := receipts.NewClient(endpoint)
		got, err := client.Fetch(t.Context(), rcpt.Link())
		require.NoError(t, err)
		require.Equal(t, rcpt.Link(), got.Link())
		require.Equal(t, inv.Link(), got.Ran())
	})

	t.Run("not found", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer server.Close()

		endpoint, err := url.Parse(server.URL)
		require.NoError(t, err)

		client := receipts.NewClient(endpoint)
		_, err = client.Fetch(t.Context(), testutil.RandomCID(t))
		require.ErrorIs(t, err, receipts.ErrNotFound)
	})

	t.Run("error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		endpoint, err := url.Parse(server.URL)
		require.NoError(t, err)

		client := receipts.NewClient(endpoint)
		_, err = client.Fetch(t.Context(), testutil.RandomCID(t))
		require.Error(t, err)
		require.ErrorContains(t, err, "500")
	})
}
