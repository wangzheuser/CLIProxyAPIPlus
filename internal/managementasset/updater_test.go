package managementasset

import (
	"bytes"
	"testing"
)

func TestApplyQuotaPaginationPatchUpdatesKnownManagementBundle(t *testing.T) {
	input := []byte("var Rb=25,zb=30,Bb=(e,t=6)=>S(g===`all`?Math.max(1,m.length):Math.min(c*3,Rb))")

	got := ApplyQuotaPaginationPatch(input)

	if bytes.Contains(got, []byte("var Rb=25,zb=30,Bb=(e,t=6)=>")) {
		t.Fatal("expected old quota pagination constants to be replaced")
	}
	if bytes.Contains(got, []byte("Math.min(c*3,Rb)")) {
		t.Fatal("expected responsive page size cap to be replaced")
	}
	if !bytes.Contains(got, []byte("var Rb=150,zb=1/0,Bb=(e,t=150)=>")) {
		t.Fatal("expected quota pagination limit to be 150 with all mode enabled")
	}
	if !bytes.Contains(got, []byte("S(g===`all`?Math.max(1,m.length):Rb)")) {
		t.Fatal("expected paged mode to use the 150 item limit directly")
	}
}

func TestApplyQuotaPaginationPatchLeavesUnknownBundleUnchanged(t *testing.T) {
	input := []byte("<html>no quota pagination bundle here</html>")

	got := ApplyQuotaPaginationPatch(input)

	if !bytes.Equal(got, input) {
		t.Fatal("expected unknown content to remain unchanged")
	}
}
