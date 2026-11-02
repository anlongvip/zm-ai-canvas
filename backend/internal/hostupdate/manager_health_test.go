package hostupdate

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

type healthTestTransport func(*http.Request) (*http.Response, error)

func (f healthTestTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestVerifyHealthyRunsFullTimeoutWindow(t *testing.T) {
	for _, failure := range []string{"readiness", "wrong version", "business error", "request timeout"} {
		t.Run(failure, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				calls := 0
				m := &Manager{config: Config{HealthURL: "http://health.invalid/ready", StableWindow: 30 * time.Second}}
				m.httpClient = &http.Client{Transport: healthTestTransport(func(r *http.Request) (*http.Response, error) {
					calls++
					status, body := 503, `{}`
					switch failure {
					case "wrong version":
						status, body = 200, `{"code":0,"data":{"build":{"version":"v1.7.3.6"}}}`
					case "business error":
						status, body = 200, `{"code":503}`
					case "request timeout":
						<-r.Context().Done()
						return nil, r.Context().Err()
					}
					return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
				})}
				started := time.Now()
				if err := m.verifyHealthy("v1.7.3.7"); err == nil || !strings.Contains(err.Error(), "未在健康检查窗口内稳定就绪") {
					t.Fatalf("unhealthy release accepted: %v", err)
				}
				elapsed := time.Since(started)
				if elapsed < 10*time.Minute || elapsed > 10*time.Minute+7*time.Second {
					t.Fatalf("verification did not run the complete window: %s", elapsed)
				}
				wantCalls := 300
				if failure == "request timeout" {
					wantCalls = 86
				}
				if calls != wantCalls {
					t.Fatalf("expected all retries: calls=%d want=%d", calls, wantCalls)
				}
				t.Logf("full verification loop: %d checks over %s of virtual time", calls, elapsed)
			})
		})
	}
}

func TestVerifyHealthyRestartsStableWindowAfterFailure(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		calls := 0
		m := &Manager{config: Config{HealthURL: "http://health.invalid/ready", StableWindow: 30 * time.Second}}
		m.httpClient = &http.Client{Transport: healthTestTransport(func(r *http.Request) (*http.Response, error) {
			calls++
			status := 200
			if calls == 3 {
				status = 503
			}
			return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"code":0,"data":{"build":{"version":"v1.7.3.7"}}}`)), Request: r}, nil
		})}
		started := time.Now()
		if err := m.verifyHealthy("v1.7.3.7"); err != nil {
			t.Fatal(err)
		}
		if elapsed := time.Since(started); elapsed != 36*time.Second || calls != 19 {
			t.Fatalf("failure did not reset the full stable window: elapsed=%s calls=%d", elapsed, calls)
		}
	})
}
