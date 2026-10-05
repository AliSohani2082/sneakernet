package probe

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/AliSohani2082/sneakernet/internal/links"
	"github.com/AliSohani2082/sneakernet/internal/xraytest"
)

// TestAllThroughLocalServer pushes real traffic through every generated
// client config to a local Xray server. No internet needed.
func TestAllThroughLocalServer(t *testing.T) {
	bin, assets := xraytest.Binary(t)
	srv := xraytest.Start(t, bin, assets)
	list := strings.Join(append(srv.Links, srv.DeadLink), "\n")
	servers, errs, err := links.ParseList(strings.NewReader(list))
	if err != nil || len(errs) != 0 {
		t.Fatalf("parse: %v %v", err, errs)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	results, err := All(ctx, bin, assets, servers, srv.ProbeURL, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range servers {
		r, ok := results[s.Index]
		if !ok {
			t.Errorf("%s: no result", s.Name)
			continue
		}
		if s.Name == "dead" {
			if r.OK() {
				t.Errorf("dead server reported working")
			}
			continue
		}
		if !r.OK() {
			t.Errorf("%s (%s): %v", s.Name, s.Kind(), r.Err)
		}
	}
}
