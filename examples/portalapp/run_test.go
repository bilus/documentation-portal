package main

import (
	"bufio"
	"bytes"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// lockedBuffer is a buffer that two goroutines may write and read.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// TestTheExampleRunsAgainstTheMocks builds the example application and the
// command of the mock providers, and runs both as the README's local run
// does: the mocks print their settings, and the example, given them, the
// demo documentation and the demo access file, signs two readers in.
func TestTheExampleRunsAgainstTheMocks(t *testing.T) {
	t.Skip("HOLE(5): run the mock providers for a local run")
	bin := t.TempDir()
	for name, pkg := range map[string]string{"portalapp": ".", "mocks": "./cmd/mocks"} {
		if out, err := exec.Command("go", "build", "-o", filepath.Join(bin, name), pkg).CombinedOutput(); err != nil {
			t.Fatalf("go build %s: %v\n%s", pkg, err, out)
		}
	}

	mockCmd := exec.CommandContext(t.Context(), filepath.Join(bin, "mocks"), "-auth0", "127.0.0.1:0", "-github", "127.0.0.1:0")
	stdout, err := mockCmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var mocksLog lockedBuffer
	mockCmd.Stderr = &mocksLog
	if err := mockCmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { mockCmd.Wait() })
	settings := map[string]string{}
	lines := make(chan string)
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			lines <- scanner.Text()
		}
		close(lines)
	}()
	names := []string{"AUTH0_ISSUER", "AUTH0_CLIENT_ID", "AUTH0_CLIENT_SECRET", "GITHUB_CLIENT_ID", "GITHUB_CLIENT_SECRET", "GITHUB_URL", "GITHUB_API_URL"}
	timeout := time.After(10 * time.Second)
	for len(settings) < len(names) {
		select {
		case line, ok := <-lines:
			if !ok {
				t.Fatalf("the mocks ended after %v: %s", settings, mocksLog.String())
			}
			if name, value, ok := strings.Cut(line, "="); ok {
				settings[name] = value
			}
		case <-timeout:
			t.Fatalf("the mocks printed %v in 10 s: %s", settings, mocksLog.String())
		}
	}
	go func() {
		for range lines {
		}
	}()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := strconv.Itoa(ln.Addr().(*net.TCPAddr).Port)
	ln.Close()
	base := "http://127.0.0.1:" + port
	_, bucket := demoBucket(t)
	access, err := filepath.Abs("demo/access.yaml")
	if err != nil {
		t.Fatal(err)
	}
	app := exec.CommandContext(t.Context(), filepath.Join(bin, "portalapp"))
	app.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + os.Getenv("HOME"),
		"PORTAL_ADDR=127.0.0.1:" + port,
		"PORTAL_URL=" + base,
		"PORTAL_BUCKET=" + bucket,
		"PORTAL_ACCESS_FILE=" + access,
		"PORTAL_SESSION_KEY=" + testKey,
	}
	for _, name := range names {
		app.Env = append(app.Env, name+"="+settings[name])
	}
	var appLog lockedBuffer
	app.Stdout, app.Stderr = &appLog, &appLog
	if err := app.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { app.Wait() })
	for deadline := time.Now().Add(15 * time.Second); ; {
		resp, err := http.Get(base + "/sign-in")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("the example never answered: %v\n%s", err, appLog.String())
		}
		time.Sleep(50 * time.Millisecond)
	}

	ada := signInAs(t, base, "auth0")
	if page := get(t, ada, base+"/portals/pets/docs/staff-notes/"); page.status != http.StatusOK || !strings.Contains(page.body, "Ada Lovelace") {
		t.Errorf("Ada's staff notes: %d %q", page.status, page.body)
	}
	if page := get(t, ada, base+"/portals/partners/specs/partner-api"); page.status != http.StatusNotFound {
		t.Errorf("the partners' API answers Ada with %d", page.status)
	}
	if page := get(t, ada, base+"/previews/pr-1"); page.status != http.StatusNotFound {
		t.Errorf("the preview answers Ada with %d", page.status)
	}

	grace := signInAs(t, base, "github")
	if page := get(t, grace, base+"/portals/partners/specs/partner-api"); page.status != http.StatusOK || !strings.Contains(page.body, "Grace Hopper") {
		t.Errorf("Grace's partners' API: %d %q", page.status, page.body)
	}
	if page := get(t, grace, base+"/portals/pets/docs/staff-notes/"); page.status != http.StatusNotFound {
		t.Errorf("the staff notes answer Grace with %d", page.status)
	}
	if page := get(t, grace, base+"/previews/pr-1/portals/pets/docs/guides/getting-started.md"); !strings.Contains(page.body, "The preview of the guide") {
		t.Errorf("Grace's preview: %d %q", page.status, page.body)
	}
	if page := get(t, grace, base+"/auth/github/sign-out"); !strings.Contains(page.body, "You have signed out") {
		t.Errorf("Grace's sign-out: %d %q", page.status, page.body)
	}
	if page := get(t, grace, base+"/"); !strings.Contains(page.body, `action="/sign-in/github"`) {
		t.Errorf("after the sign-out, Grace's next page: %s %q", page.url, page.body)
	}
}
