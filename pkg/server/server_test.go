package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fil-forge/ucantone/testutil"
	"github.com/stretchr/testify/require"

	"github.com/fil-forge/piri/pkg/build"
)

func TestVersionInfoHandler(t *testing.T) {
	id := testutil.RandomIssuer(t)

	ts := httptest.NewServer(NewHandler(id))
	defer ts.Close()

	t.Run("text/plain", func(t *testing.T) {
		res, err := http.Get(ts.URL)
		require.NoError(t, err)

		body, err := io.ReadAll(res.Body)
		res.Body.Close()
		require.NoError(t, err)

		require.Contains(t, string(body), id.DID().String())
		require.Contains(t, string(body), build.Version)
	})

	t.Run("application/json", func(t *testing.T) {
		req, err := http.NewRequest("GET", ts.URL, nil)
		require.NoError(t, err)
		req.Header.Set("Accept", "application/json")

		res, err := http.DefaultClient.Do(req)
		require.NoError(t, err)

		body, err := io.ReadAll(res.Body)
		res.Body.Close()
		require.NoError(t, err)

		info := ServerInfo{}
		err = json.Unmarshal(body, &info)
		require.NoError(t, err)

		require.Equal(t, id.DID().String(), info.ID)
		require.Equal(t, build.Version, info.Build.Version)
	})
}
