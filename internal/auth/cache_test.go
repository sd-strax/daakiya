package auth

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/AzureAD/microsoft-authentication-library-for-go/apps/cache"

	"github.com/sd-strax/daakiya/internal/secrets"
)

type blob struct{ b []byte }

func (b *blob) Marshal() ([]byte, error) { return b.b, nil }
func (b *blob) Unmarshal(d []byte) error { b.b = append([]byte(nil), d...); return nil }

func TestFileCacheRoundTrip(t *testing.T) {
	sec := secrets.NewMemory()
	fc := fileCache{secrets: sec, path: filepath.Join(t.TempDir(), "c.bin")}
	ctx := context.Background()
	// Empty cache: Replace is a no-op.
	var in blob
	if err := fc.Replace(ctx, &in, cacheReplaceHints()); err != nil || in.b != nil {
		t.Fatalf("empty replace: %v %v", err, in.b)
	}
	big := make([]byte, 20000)
	for i := range big {
		big[i] = byte(i)
	}
	if err := fc.Export(ctx, &blob{b: big}, cacheExportHints()); err != nil {
		t.Fatal(err)
	}
	var out blob
	if err := fc.Replace(ctx, &out, cacheReplaceHints()); err != nil || string(out.b) != string(big) {
		t.Fatalf("round trip failed: %v", err)
	}
	if k, _ := sec.Get(AccountCacheKey); len(k) < 40 {
		t.Error("key not stored in secrets")
	}
	if err := fc.Clear(); err != nil {
		t.Fatal(err)
	}
	var after blob
	if err := fc.Replace(ctx, &after, cacheReplaceHints()); err != nil || after.b != nil {
		t.Errorf("after clear: %v %v", err, after.b)
	}
}

func cacheReplaceHints() cache.ReplaceHints { return cache.ReplaceHints{} }
func cacheExportHints() cache.ExportHints   { return cache.ExportHints{} }
