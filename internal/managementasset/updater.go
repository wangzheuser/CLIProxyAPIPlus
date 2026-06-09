package managementasset

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/util"
	sdkconfig "github.com/router-for-me/CLIProxyAPI/v7/sdk/config"
	log "github.com/sirupsen/logrus"
	"golang.org/x/sync/singleflight"
)

const (
	defaultManagementReleaseURL  = "https://api.github.com/repos/router-for-me/Cli-Proxy-API-Management-Center/releases/latest"
	defaultManagementFallbackURL = "https://cpamc.router-for.me/"
	managementAssetName          = "management.html"
	httpUserAgent                = "CLIProxyAPI-management-updater"
	managementSyncMinInterval    = 30 * time.Second
	updateCheckInterval          = 3 * time.Hour
	maxAssetDownloadSize         = 50 << 20 // 10 MB safety limit for management asset downloads
)

// ManagementFileName exposes the control panel asset filename.
const ManagementFileName = managementAssetName

var (
	lastUpdateCheckMu   sync.Mutex
	lastUpdateCheckTime time.Time
	currentConfigPtr    atomic.Pointer[config.Config]
	schedulerOnce       sync.Once
	schedulerConfigPath atomic.Value
	sfGroup             singleflight.Group
)

// SetCurrentConfig stores the latest configuration snapshot for management asset decisions.
func SetCurrentConfig(cfg *config.Config) {
	if cfg == nil {
		currentConfigPtr.Store(nil)
		return
	}
	currentConfigPtr.Store(cfg)
}

// StartAutoUpdater launches a background goroutine that periodically ensures the management asset is up to date.
// It respects the disable-control-panel flag on every iteration and supports hot-reloaded configurations.
func StartAutoUpdater(ctx context.Context, configFilePath string) {
	configFilePath = strings.TrimSpace(configFilePath)
	if configFilePath == "" {
		log.Debug("management asset auto-updater skipped: empty config path")
		return
	}

	schedulerConfigPath.Store(configFilePath)

	schedulerOnce.Do(func() {
		go runAutoUpdater(ctx)
	})
}

func runAutoUpdater(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}

	ticker := time.NewTicker(updateCheckInterval)
	defer ticker.Stop()

	runOnce := func() {
		cfg := currentConfigPtr.Load()
		if cfg == nil {
			log.Debug("management asset auto-updater skipped: config not yet available")
			return
		}
		if cfg.RemoteManagement.DisableControlPanel {
			log.Debug("management asset auto-updater skipped: control panel disabled")
			return
		}
		if cfg.RemoteManagement.DisableAutoUpdatePanel {
			log.Debug("management asset auto-updater skipped: disable-auto-update-panel is enabled")
			return
		}

		configPath, _ := schedulerConfigPath.Load().(string)
		staticDir := StaticDir(configPath)
		EnsureLatestManagementHTML(ctx, staticDir, cfg.ProxyURL, cfg.RemoteManagement.PanelGitHubRepository)
	}

	runOnce()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			runOnce()
		}
	}
}

func newHTTPClient(proxyURL string) *http.Client {
	client := &http.Client{Timeout: 15 * time.Second}

	sdkCfg := &sdkconfig.SDKConfig{ProxyURL: strings.TrimSpace(proxyURL)}
	util.SetProxy(sdkCfg, client)

	return client
}

type releaseAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Digest             string `json:"digest"`
}

type releaseResponse struct {
	Assets []releaseAsset `json:"assets"`
}

// StaticDir resolves the directory that stores the management control panel asset.
func StaticDir(configFilePath string) string {
	if override := strings.TrimSpace(os.Getenv("MANAGEMENT_STATIC_PATH")); override != "" {
		cleaned := filepath.Clean(override)
		if strings.EqualFold(filepath.Base(cleaned), managementAssetName) {
			return filepath.Dir(cleaned)
		}
		return cleaned
	}

	if writable := util.WritablePath(); writable != "" {
		return filepath.Join(writable, "static")
	}

	configFilePath = strings.TrimSpace(configFilePath)
	if configFilePath == "" {
		return ""
	}

	base := filepath.Dir(configFilePath)
	fileInfo, err := os.Stat(configFilePath)
	if err == nil {
		if fileInfo.IsDir() {
			base = configFilePath
		}
	}

	return filepath.Join(base, "static")
}

// FilePath resolves the absolute path to the management control panel asset.
func FilePath(configFilePath string) string {
	if override := strings.TrimSpace(os.Getenv("MANAGEMENT_STATIC_PATH")); override != "" {
		cleaned := filepath.Clean(override)
		if strings.EqualFold(filepath.Base(cleaned), managementAssetName) {
			return cleaned
		}
		return filepath.Join(cleaned, ManagementFileName)
	}

	dir := StaticDir(configFilePath)
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, ManagementFileName)
}

// ApplyQuotaPaginationPatch keeps local management UI fixes applied to upstream bundles.
func ApplyQuotaPaginationPatch(data []byte) []byte {
	if len(data) == 0 {
		return data
	}

	html := string(data)
	patched := html

	for _, replacement := range []struct {
		old string
		new string
	}{
		{
			old: "function Pb({item:e,quota:t,resolvedTheme:n,i18nPrefix:r,cardIdleMessageKey:i,cardClassName:a,defaultType:o,canRefresh:s=!1,onRefresh:c,renderQuotaItems:l}){let{t:u}=qo(),d=",
			new: "function Pb({item:e,quota:t,resolvedTheme:n,i18nPrefix:r,cardIdleMessageKey:i,cardClassName:a,defaultType:o,canRefresh:s=!1,onRefresh:c,canDelete:P=!1,onDelete:F,deleting:ne=!1,renderQuotaItems:l}){let{t:u}=qo(),d=",
		},
		{
			old: "children:[(0,I.jsx)(`span`,{className:U.typeBadge,style:{backgroundColor:p.bg,color:p.text,...p.border?{border:p.border}:{}},children:_(d)}),(0,I.jsx)(`span`,{className:U.fileName,children:e.name})]}),(0,I.jsx)(`div`,{className:U.quotaSection",
			new: "children:[(0,I.jsx)(`span`,{className:U.typeBadge,style:{backgroundColor:p.bg,color:p.text,...p.border?{border:p.border}:{}},children:_(d)}),(0,I.jsx)(`span`,{className:U.fileName,style:{flex:1,minWidth:0},children:e.name}),F&&(0,I.jsx)(L,{variant:`danger`,size:`sm`,onClick:F,disabled:!P||ne,title:u(`auth_files.delete_button`),children:ne?(0,I.jsx)(gy,{size:14}):u(`common.delete`)})]}),(0,I.jsx)(`div`,{className:U.quotaSection",
		},
		{
			old: "function Vb({config:e,files:t,loading:n,disabled:r}){let{t:i}=qo(),a=jc(e=>e.resolvedTheme),o=Sc(e=>e.showNotification),s=lp(t=>t[e.storeSetter]),[c,l]=Lb(380)",
			new: "function Vb({config:e,files:t,loading:n,disabled:r,onDeleted:q}){let{t:i}=qo(),a=jc(e=>e.resolvedTheme),o=Sc(e=>e.showNotification),P=Sc(e=>e.showConfirmation),s=lp(t=>t[e.storeSetter]),[F,ne]=(0,y.useState)(null),[re,ie]=(0,y.useState)(`all`),[ae,oe]=(0,y.useState)(!1),{quota:D,loadQuota:O}=Ib(e),[c,l]=Lb(380)",
		},
		{
			old: "m=(0,y.useMemo)(()=>t.filter(t=>e.filterFn(t)),[t,e]),h=m.length<=zb,g=u===`all`&&!h?`paged`:u,{pageSize:_,totalPages:v,currentPage:b,pageItems:x,setPageSize:S,goToPrev:C,goToNext:w,loading:T,setLoading:E}=Bb(m);",
			new: "m=(0,y.useMemo)(()=>t.filter(t=>e.filterFn(t)),[t,e]),h=(0,y.useMemo)(()=>m.reduce((e,t)=>e+(D[t.name]?.status===`success`?1:0),0),[m,D]),g=(0,y.useMemo)(()=>m.reduce((e,t)=>e+(D[t.name]?.status===`error`?1:0),0),[m,D]),se=(0,y.useMemo)(()=>re===`normal`?m.filter(e=>D[e.name]?.status===`success`):re===`error`?m.filter(e=>D[e.name]?.status===`error`):m,[m,D,re]),ce=se.length<=zb,le=u===`all`&&!ce?`paged`:u,{pageSize:_,totalPages:v,currentPage:b,pageItems:x,setPageSize:S,goToPrev:C,goToNext:w,loading:T,setLoading:E}=Bb(se);",
		},
		{
			old: "if(h||u!==`all`)return;",
			new: "if(ce||u!==`all`)return;",
		},
		{
			old: "},[h,u]),",
			new: "},[ce,u]),",
		},
		{
			old: "S(g===`all`?Math.max(1,m.length):Rb)",
			new: "S(le===`all`?Math.max(1,se.length):Rb)",
		},
		{
			old: "},[g,c,m.length,S]);let{quota:D,loadQuota:O}=Ib(e),k=",
			new: "},[le,c,se.length,S]);let k=",
		},
		{
			old: "let t=g===`all`?`all`:`page`,r=g===`all`?m:x;r.length!==0&&O(r,t,E)",
			new: "let t=le===`all`?`all`:`page`,r=le===`all`?se:x;r.length!==0&&O(r,t,E)",
		},
		{
			old: "},[n,g,m,x,O,E])",
			new: "},[n,le,se,x,O,E])",
		},
		{
			old: "},[e,r,D,s,o,i]),M=(0,I.jsxs)(`div`,{className:U.titleWrapper",
			new: "},[e,r,D,s,o,i]),te=(0,y.useCallback)(t=>{if(r||F)return;P({title:i(`auth_files.delete_title`,{defaultValue:`Delete File`}),message:`${i(`auth_files.delete_confirm`)} \"${t.name}\" ?`,variant:`danger`,confirmText:i(`common.confirm`),onConfirm:async()=>{ne(t.name);try{await Gh.deleteFile(t.name),o(i(`auth_files.delete_success`),`success`),s(e=>{let n={...e};return delete n[t.name],n});if(q)try{await q()}catch(t){let n=t instanceof Error?t.message:``;o(`${i(`notification.refresh_failed`)}: ${n}`,`error`)}}catch(t){let n=t instanceof Error?t.message:``;o(`${i(`notification.delete_failed`)}: ${n}`,`error`)}finally{ne(null)}}})},[r,F,P,i,o,s,q]),M=(0,I.jsxs)(`div`,{className:U.titleWrapper",
		},
		{
			old: "canRefresh:!r&&!t.disabled,onRefresh:()=>void ee(t),renderQuotaItems:e.renderQuotaItems}",
			new: "canRefresh:!r&&!t.disabled,onRefresh:()=>void ee(t),canDelete:!r,onDelete:()=>te(t),deleting:F===t.name,renderQuotaItems:e.renderQuotaItems}",
		},
		{
			old: "m.length>0&&(0,I.jsx)(`span`,{className:U.countBadge,children:m.length})",
			new: "m.length>0&&(0,I.jsx)(`span`,{className:U.countBadge,children:se.length})",
		},
		{
			old: "children:[(0,I.jsxs)(`div`,{className:U.viewModeToggle",
			new: "children:[(0,I.jsxs)(`select`,{className:U.pageSizeSelect,value:re,onChange:e=>ie(e.target.value),disabled:r,title:i(`quota_management.status_filter_label`),\"aria-label\":i(`quota_management.status_filter_label`),children:[(0,I.jsx)(`option`,{value:`all`,children:`${i(`quota_management.status_filter_all`)} (${m.length})`}),(0,I.jsx)(`option`,{value:`normal`,children:`${i(`quota_management.status_filter_normal`)} (${h})`}),(0,I.jsx)(`option`,{value:`error`,children:`${i(`quota_management.status_filter_error`)} (${g})`})]}),(0,I.jsx)(L,{variant:`danger`,size:`sm`,onClick:()=>{if(r||ae||N)return;let e=re===`normal`?i(`quota_management.status_filter_normal`):re===`error`?i(`quota_management.status_filter_error`):i(`quota_management.status_filter_all`);se.length===0?o(i(`quota_management.delete_filtered_none`),`warning`):P({title:i(`quota_management.delete_filtered_button`),message:i(`quota_management.delete_filtered_confirm`,{scope:e,count:se.length}),variant:`danger`,confirmText:i(`common.confirm`),onConfirm:async()=>{oe(!0);let e=se.map(e=>e.name);try{let t=await Gh.deleteFiles(e),n=new Set((t.failed??[]).map(e=>String(e?.name??``).trim()).filter(Boolean)),r=t.files&&t.files.length?t.files:e.filter(e=>!n.has(e)),a=t.deleted??r.length;r.length&&s(e=>{let t={...e};return r.forEach(e=>{delete t[e]}),t});if(q)try{await q()}catch(e){let t=e instanceof Error?e.message:``;o(`${i(`notification.refresh_failed`)}: ${t}`,`error`)}(t.failed?.length??0)>0?o(i(`quota_management.delete_filtered_partial`,{success:a,failed:t.failed.length}),`warning`):o(i(`quota_management.delete_filtered_success`,{count:a}),`success`)}catch(e){let t=e instanceof Error?e.message:i(`common.unknown_error`);o(`${i(`quota_management.delete_filtered_failed`)}: ${t}`,`error`)}finally{oe(!1)}}})},disabled:r||ae||N||se.length===0,loading:ae,title:i(`quota_management.delete_filtered_title`,{scope:re===`normal`?i(`quota_management.status_filter_normal`):re===`error`?i(`quota_management.status_filter_error`):i(`quota_management.status_filter_all`),count:se.length}),children:i(`quota_management.delete_filtered_button`)}),(0,I.jsxs)(`div`,{className:U.viewModeToggle",
		},
		{
			old: "${U.viewModeButton} ${g===`paged`?U.viewModeButtonActive:``}",
			new: "${U.viewModeButton} ${le===`paged`?U.viewModeButtonActive:``}",
		},
		{
			old: "${U.viewModeButton} ${g===`all`?U.viewModeButtonActive:``}",
			new: "${U.viewModeButton} ${le===`all`?U.viewModeButtonActive:``}",
		},
		{
			old: "m.length>zb?p(!0):d(`all`)",
			new: "se.length>zb?p(!0):d(`all`)",
		},
		{
			old: "m.length===0?(0,I.jsx)(Bv,{title:i(`${e.i18nPrefix}.empty_title`),description:i(`${e.i18nPrefix}.empty_desc`)})",
			new: "se.length===0?(0,I.jsx)(Bv,{title:i(m.length===0?`${e.i18nPrefix}.empty_title`:`quota_management.status_filter_empty_title`),description:i(m.length===0?`${e.i18nPrefix}.empty_desc`:`quota_management.status_filter_empty_desc`)})",
		},
		{
			old: "count:m.length",
			new: "count:se.length",
		},
		{
			old: "m.length>_&&g===`paged`",
			new: "se.length>0&&le===`paged`",
		},
		{
			old: "var Rb=25,zb=30,Bb=(e,t=6)=>",
			new: "var Rb=100,zb=1/0,Bb=(e,t=100)=>",
		},
		{
			old: "var Rb=150,zb=1/0,Bb=",
			new: "var Rb=100,zb=1/0,Bb=",
		},
		{
			old: "Bb=(e,t=150)=>",
			new: "Bb=(e,t=100)=>",
		},
		{
			old: "Math.min(c*3,Rb)",
			new: "Rb",
		},
		{
			old: "ty=e=>Math.min(30,Math.max(3,Math.round(e)))",
			new: "ty=e=>Math.min(100,Math.max(3,Math.round(e)))",
		},
		{
			old: "i<3||i>30||",
			new: "i<3||i>100||",
		},
		{
			old: "i<3||i>30",
			new: "i<3||i>100",
		},
		{
			old: "min:3,max:30,step:1",
			new: "min:3,max:100,step:1",
		},
		{
			old: "max:30,step:1",
			new: "max:100,step:1",
		},
		{
			old: "(0,I.jsx)(Vb,{config:hx,files:n,loading:i,disabled:c})",
			new: "(0,I.jsx)(Vb,{config:hx,files:n,loading:i,disabled:c,onDeleted:u})",
		},
		{
			old: "(0,I.jsx)(Vb,{config:gx,files:n,loading:i,disabled:c})",
			new: "(0,I.jsx)(Vb,{config:gx,files:n,loading:i,disabled:c,onDeleted:u})",
		},
		{
			old: "(0,I.jsx)(Vb,{config:_x,files:n,loading:i,disabled:c})",
			new: "(0,I.jsx)(Vb,{config:_x,files:n,loading:i,disabled:c,onDeleted:u})",
		},
		{
			old: "(0,I.jsx)(Vb,{config:Ox,files:n,loading:i,disabled:c})",
			new: "(0,I.jsx)(Vb,{config:Ox,files:n,loading:i,disabled:c,onDeleted:u})",
		},
		{
			old: "(0,I.jsx)(Vb,{config:vx,files:n,loading:i,disabled:c})",
			new: "(0,I.jsx)(Vb,{config:vx,files:n,loading:i,disabled:c,onDeleted:u})",
		},
		{
			old: "(0,I.jsx)(Vb,{config:Dx,files:n,loading:i,disabled:c})",
			new: "(0,I.jsx)(Vb,{config:Dx,files:n,loading:i,disabled:c,onDeleted:u})",
		},
	} {
		patched = strings.ReplaceAll(patched, replacement.old, replacement.new)
	}

	if patched == html {
		return data
	}
	return []byte(patched)
}

// EnsureLatestManagementHTML checks the latest management.html asset and updates the local copy when needed.
// It coalesces concurrent sync attempts and returns whether the asset exists after the sync attempt.
func EnsureLatestManagementHTML(ctx context.Context, staticDir string, proxyURL string, panelRepository string) bool {
	if ctx == nil {
		ctx = context.Background()
	}

	staticDir = strings.TrimSpace(staticDir)
	if staticDir == "" {
		log.Debug("management asset sync skipped: empty static directory")
		return false
	}
	localPath := filepath.Join(staticDir, managementAssetName)

	_, _, _ = sfGroup.Do(localPath, func() (interface{}, error) {
		lastUpdateCheckMu.Lock()
		now := time.Now()
		timeSinceLastAttempt := now.Sub(lastUpdateCheckTime)
		if !lastUpdateCheckTime.IsZero() && timeSinceLastAttempt < managementSyncMinInterval {
			lastUpdateCheckMu.Unlock()
			log.Debugf(
				"management asset sync skipped by throttle: last attempt %v ago (interval %v)",
				timeSinceLastAttempt.Round(time.Second),
				managementSyncMinInterval,
			)
			return nil, nil
		}
		lastUpdateCheckTime = now
		lastUpdateCheckMu.Unlock()

		localFileMissing := false
		if _, errStat := os.Stat(localPath); errStat != nil {
			if errors.Is(errStat, os.ErrNotExist) {
				localFileMissing = true
			} else {
				log.WithError(errStat).Debug("failed to stat local management asset")
			}
		}

		if errMkdirAll := os.MkdirAll(staticDir, 0o755); errMkdirAll != nil {
			log.WithError(errMkdirAll).Warn("failed to prepare static directory for management asset")
			return nil, nil
		}

		releaseURL := resolveReleaseURL(panelRepository)
		client := newHTTPClient(proxyURL)

		localHash, err := fileSHA256(localPath)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				log.WithError(err).Debug("failed to read local management asset hash")
			}
			localHash = ""
		}

		asset, remoteHash, err := fetchLatestAsset(ctx, client, releaseURL)
		if err != nil {
			if localFileMissing {
				log.WithError(err).Warn("failed to fetch latest management release information, trying fallback page")
				if ensureFallbackManagementHTML(ctx, client, localPath) {
					return nil, nil
				}
				return nil, nil
			}
			log.WithError(err).Warn("failed to fetch latest management release information")
			return nil, nil
		}

		if remoteHash != "" && localHash != "" && strings.EqualFold(remoteHash, localHash) {
			log.Debug("management asset is already up to date")
			return nil, nil
		}

		data, downloadedHash, err := downloadAsset(ctx, client, asset.BrowserDownloadURL)
		if err != nil {
			if localFileMissing {
				log.WithError(err).Warn("failed to download management asset, trying fallback page")
				if ensureFallbackManagementHTML(ctx, client, localPath) {
					return nil, nil
				}
				return nil, nil
			}
			log.WithError(err).Warn("failed to download management asset")
			return nil, nil
		}

		if remoteHash != "" && !strings.EqualFold(remoteHash, downloadedHash) {
			log.Errorf("management asset digest mismatch: expected %s got %s — aborting update for safety", remoteHash, downloadedHash)
			return nil, nil
		}

		if err = atomicWriteFile(localPath, data); err != nil {
			log.WithError(err).Warn("failed to update management asset on disk")
			return nil, nil
		}

		log.Infof("management asset updated successfully (hash=%s)", downloadedHash)
		return nil, nil
	})

	_, err := os.Stat(localPath)
	return err == nil
}

func ensureFallbackManagementHTML(ctx context.Context, client *http.Client, localPath string) bool {
	data, downloadedHash, err := downloadAsset(ctx, client, defaultManagementFallbackURL)
	if err != nil {
		log.WithError(err).Warn("failed to download fallback management control panel page")
		return false
	}

	log.Warnf("management asset downloaded from fallback URL without digest verification (hash=%s) — "+
		"enable verified GitHub updates by keeping disable-auto-update-panel set to false", downloadedHash)

	if err = atomicWriteFile(localPath, data); err != nil {
		log.WithError(err).Warn("failed to persist fallback management control panel page")
		return false
	}

	log.Infof("management asset updated from fallback page successfully (hash=%s)", downloadedHash)
	return true
}

func resolveReleaseURL(repo string) string {
	repo = strings.TrimSpace(repo)
	if repo == "" {
		return defaultManagementReleaseURL
	}

	parsed, err := url.Parse(repo)
	if err != nil || parsed.Host == "" {
		return defaultManagementReleaseURL
	}

	host := strings.ToLower(parsed.Host)
	parsed.Path = strings.TrimSuffix(parsed.Path, "/")

	if host == "api.github.com" {
		if !strings.HasSuffix(strings.ToLower(parsed.Path), "/releases/latest") {
			parsed.Path = parsed.Path + "/releases/latest"
		}
		return parsed.String()
	}

	if host == "github.com" {
		parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
		if len(parts) >= 2 && parts[0] != "" && parts[1] != "" {
			repoName := strings.TrimSuffix(parts[1], ".git")
			return fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", parts[0], repoName)
		}
	}

	return defaultManagementReleaseURL
}

func fetchLatestAsset(ctx context.Context, client *http.Client, releaseURL string) (*releaseAsset, string, error) {
	if strings.TrimSpace(releaseURL) == "" {
		releaseURL = defaultManagementReleaseURL
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, releaseURL, nil)
	if err != nil {
		return nil, "", fmt.Errorf("create release request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", httpUserAgent)
	gitURL := strings.ToLower(strings.TrimSpace(os.Getenv("GITSTORE_GIT_URL")))
	if tok := strings.TrimSpace(os.Getenv("GITSTORE_GIT_TOKEN")); tok != "" && strings.Contains(gitURL, "github.com") {
		req.Header.Set("Authorization", "Bearer "+tok)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("execute release request: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, "", fmt.Errorf("unexpected release status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var release releaseResponse
	if err = json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, "", fmt.Errorf("decode release response: %w", err)
	}

	for i := range release.Assets {
		asset := &release.Assets[i]
		if strings.EqualFold(asset.Name, managementAssetName) {
			remoteHash := parseDigest(asset.Digest)
			return asset, remoteHash, nil
		}
	}

	return nil, "", fmt.Errorf("management asset %s not found in latest release", managementAssetName)
}

func downloadAsset(ctx context.Context, client *http.Client, downloadURL string) ([]byte, string, error) {
	if strings.TrimSpace(downloadURL) == "" {
		return nil, "", fmt.Errorf("empty download url")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return nil, "", fmt.Errorf("create download request: %w", err)
	}
	req.Header.Set("User-Agent", httpUserAgent)

	resp, err := client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("execute download request: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, "", fmt.Errorf("unexpected download status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxAssetDownloadSize+1))
	if err != nil {
		return nil, "", fmt.Errorf("read download body: %w", err)
	}
	if int64(len(data)) > maxAssetDownloadSize {
		return nil, "", fmt.Errorf("download exceeds maximum allowed size of %d bytes", maxAssetDownloadSize)
	}

	sum := sha256.Sum256(data)
	return data, hex.EncodeToString(sum[:]), nil
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() {
		_ = file.Close()
	}()

	h := sha256.New()
	if _, err = io.Copy(h, file); err != nil {
		return "", err
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

func atomicWriteFile(path string, data []byte) error {
	tmpFile, err := os.CreateTemp(filepath.Dir(path), "management-*.html")
	if err != nil {
		return err
	}

	tmpName := tmpFile.Name()
	defer func() {
		_ = tmpFile.Close()
		_ = os.Remove(tmpName)
	}()

	if _, err = tmpFile.Write(data); err != nil {
		return err
	}

	if err = tmpFile.Chmod(0o644); err != nil {
		return err
	}

	if err = tmpFile.Close(); err != nil {
		return err
	}

	if err = os.Rename(tmpName, path); err != nil {
		return err
	}

	return nil
}

func parseDigest(digest string) string {
	digest = strings.TrimSpace(digest)
	if digest == "" {
		return ""
	}

	if idx := strings.Index(digest, ":"); idx >= 0 {
		digest = digest[idx+1:]
	}

	return strings.ToLower(strings.TrimSpace(digest))
}
