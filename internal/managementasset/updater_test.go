package managementasset

import (
	"bytes"
	"strings"
	"testing"
)

func TestApplyQuotaPaginationPatchUpdatesQuotaPagination(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		oldParts []string
		newParts []string
	}{
		{
			name:  "old upstream bundle",
			input: "var Rb=25,zb=30,Bb=(e,t=6)=>S(g===`all`?Math.max(1,m.length):Math.min(c*3,Rb))m.length>_&&g===`paged`",
			oldParts: []string{
				"var Rb=25,zb=30,Bb=(e,t=6)=>",
				"Math.min(c*3,Rb)",
				"m.length>_&&g===`paged`",
			},
			newParts: []string{
				"var Rb=100,zb=1/0,Bb=(e,t=100)=>",
				"S(le===`all`?Math.max(1,se.length):Rb)",
				"se.length>0&&le===`paged`",
			},
		},
		{
			name:  "intermediate 150 bundle",
			input: "var Rb=150,zb=1/0,Bb=(e,t=150)=>S(g===`all`?Math.max(1,m.length):Rb)",
			oldParts: []string{
				"var Rb=150,zb=1/0,Bb=",
				"Bb=(e,t=150)=>",
			},
			newParts: []string{
				"var Rb=100,zb=1/0,Bb=(e,t=100)=>",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ApplyQuotaPaginationPatch([]byte(tt.input))

			for _, old := range tt.oldParts {
				if bytes.Contains(got, []byte(old)) {
					t.Fatalf("expected %q to be replaced", old)
				}
			}
			for _, expected := range tt.newParts {
				if !bytes.Contains(got, []byte(expected)) {
					t.Fatalf("expected patched content to contain %q", expected)
				}
			}
		})
	}
}

func TestApplyQuotaPaginationPatchUpdatesAuthFilePageSizeLimit(t *testing.T) {
	input := []byte("ty=e=>Math.min(30,Math.max(3,Math.round(e))) i<3||i>30|| min:3,max:30,step:1")

	got := ApplyQuotaPaginationPatch(input)

	for _, old := range []string{
		"ty=e=>Math.min(30,Math.max(3,Math.round(e)))",
		"i<3||i>30||",
		"min:3,max:30,step:1",
	} {
		if bytes.Contains(got, []byte(old)) {
			t.Fatalf("expected %q to be replaced", old)
		}
	}
	for _, expected := range []string{
		"ty=e=>Math.min(100,Math.max(3,Math.round(e)))",
		"i<3||i>100||",
		"min:3,max:100,step:1",
	} {
		if !bytes.Contains(got, []byte(expected)) {
			t.Fatalf("expected patched content to contain %q", expected)
		}
	}
}

func TestApplyQuotaPaginationPatchAddsQuotaCardDeleteControls(t *testing.T) {
	input := []byte(strings.Join([]string{
		"function Pb({item:e,quota:t,resolvedTheme:n,i18nPrefix:r,cardIdleMessageKey:i,cardClassName:a,defaultType:o,canRefresh:s=!1,onRefresh:c,renderQuotaItems:l}){let{t:u}=qo(),d=",
		"children:[(0,I.jsx)(`span`,{className:U.typeBadge,style:{backgroundColor:p.bg,color:p.text,...p.border?{border:p.border}:{}},children:_(d)}),(0,I.jsx)(`span`,{className:U.fileName,children:e.name})]}),(0,I.jsx)(`div`,{className:U.quotaSection",
		"function Vb({config:e,files:t,loading:n,disabled:r}){let{t:i}=qo(),a=jc(e=>e.resolvedTheme),o=Sc(e=>e.showNotification),s=lp(t=>t[e.storeSetter]),[c,l]=Lb(380)",
		"},[e,r,D,s,o,i]),M=(0,I.jsxs)(`div`,{className:U.titleWrapper",
		"canRefresh:!r&&!t.disabled,onRefresh:()=>void ee(t),renderQuotaItems:e.renderQuotaItems}",
		"(0,I.jsx)(Vb,{config:hx,files:n,loading:i,disabled:c})",
	}, "\n"))

	got := ApplyQuotaPaginationPatch(input)

	for _, old := range []string{
		"canRefresh:s=!1,onRefresh:c,renderQuotaItems:l",
		"function Vb({config:e,files:t,loading:n,disabled:r})",
		"canRefresh:!r&&!t.disabled,onRefresh:()=>void ee(t),renderQuotaItems:e.renderQuotaItems}",
	} {
		if bytes.Contains(got, []byte(old)) {
			t.Fatalf("expected %q to be replaced", old)
		}
	}
	for _, expected := range []string{
		"canRefresh:s=!1,onRefresh:c,canDelete:P=!1,onDelete:F,deleting:ne=!1,renderQuotaItems:l",
		"style:{flex:1,minWidth:0},children:e.name",
		"F&&(0,I.jsx)(L,{variant:`danger`,size:`sm`,onClick:F,disabled:!P||ne,title:u(`auth_files.delete_button`),children:ne?(0,I.jsx)(gy,{size:14}):u(`common.delete`)})",
		"function Vb({config:e,files:t,loading:n,disabled:r,onDeleted:q})",
		"P=Sc(e=>e.showConfirmation)",
		"[F,ne]=(0,y.useState)(null)",
		"te=(0,y.useCallback)(t=>{if(r||F)return;P({title:i(`auth_files.delete_title`",
		"await Gh.deleteFile(t.name)",
		"canDelete:!r,onDelete:()=>te(t),deleting:F===t.name,renderQuotaItems:e.renderQuotaItems}",
		"(0,I.jsx)(Vb,{config:hx,files:n,loading:i,disabled:c,onDeleted:u})",
	} {
		if !bytes.Contains(got, []byte(expected)) {
			t.Fatalf("expected patched content to contain %q", expected)
		}
	}
}

func TestApplyQuotaPaginationPatchLeavesUnknownBundleUnchanged(t *testing.T) {
	input := []byte("<html>no quota pagination bundle here</html>")

	got := ApplyQuotaPaginationPatch(input)

	if !bytes.Equal(got, input) {
		t.Fatal("expected unknown content to remain unchanged")
	}
}
