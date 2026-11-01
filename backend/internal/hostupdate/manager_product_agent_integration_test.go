package hostupdate

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Exercise the published remote Agent, real model-task worker, shutdown and
// backup/rollback together. Only the model upstream and fixture identity are fake.
func TestProductActiveAgentRollback(t *testing.T) {
	if os.Getenv("CANVAS_HOSTUPDATE_PRODUCT_TEST") != "1" {
		t.Skip("set CANVAS_HOSTUPDATE_PRODUCT_TEST=1 with cached release images")
	}
	version := os.Getenv("CANVAS_HOSTUPDATE_PRODUCT_VERSION")
	source := os.Getenv("CANVAS_HOSTUPDATE_PRODUCT_COMPOSE")
	repository := os.Getenv("CANVAS_HOSTUPDATE_PRODUCT_REPOSITORY")
	if !IsReleaseVersion(version) || source == "" || repository == "" {
		t.Fatal("fixed product version, release Compose and repository are required")
	}
	dir := filepath.Join(t.TempDir(), "canvas-product-"+randomID())
	m, body := productContainerFixture(t, dir, source, repository, version)
	var config map[string]any
	if err := json.Unmarshal([]byte(body), &config); err != nil {
		t.Fatal(err)
	}
	services := config["services"].(map[string]any)
	if binary := os.Getenv("CANVAS_HOSTUPDATE_PRODUCT_BACKEND_BINARY"); binary != "" {
		info, err := os.Lstat(binary)
		if err != nil || !filepath.IsAbs(binary) || !info.Mode().IsRegular() {
			t.Fatalf("backend override must be an absolute regular file: %s (%v)", binary, err)
		}
		body, err := os.ReadFile(binary)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("isolated backend binary override SHA-256=%x; other release image contents unchanged", sha256.Sum256(body))
		backend := services["backend"].(map[string]any)
		mounts, _ := backend["volumes"].([]any)
		backend["volumes"] = append(mounts, map[string]any{"type": "bind", "source": binary, "target": "/usr/local/bin/yingce-backend", "read_only": true})
	}
	services["backend"].(map[string]any)["environment"].(map[string]any)["CANVAS_ALLOWED_PRIVATE_UPSTREAM_HOSTS"] = "model-fixture"
	services["model-fixture"] = map[string]any{
		"image":      immutableImageRefs(repository, version).agent,
		"entrypoint": []string{"node"}, "command": []string{"-e", productAgentUpstream},
		"restart": "no", "mem_limit": "128m",
		"healthcheck": map[string]any{
			"test":     []string{"CMD", "node", "-e", "fetch('http://127.0.0.1:18080/status').then(r=>{if(!r.ok)process.exit(1)}).catch(()=>process.exit(1))"},
			"interval": "1s", "timeout": "3s", "retries": 30, "start_period": "1s",
		},
	}
	encoded, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(m.composePath(), encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	m.httpClient.Transport = productHealthTransport{manager: m, version: version, compose: string(encoded)}
	t.Cleanup(func() {
		m.runner = execRunner{dir: dir}
		if t.Failed() {
			var logs bytes.Buffer
			_ = m.compose(m.composePath(), version, time.Minute, &logs, "logs", "--no-color", "--tail", "60", "backend", "yingce-agent", "model-fixture")
			t.Log(logs.String())
		}
		if err := m.compose(m.composePath(), version, 2*time.Minute, nil, "down", "--volumes", "--remove-orphans"); err != nil {
			t.Errorf("clean active-Agent fixture: %v", err)
		}
	})
	productCompose(t, m, version, "up", "-d", "--pull", "never", "--wait", "--wait-timeout", "120")
	if err := m.checkHealthOnce(m.healthURL(), version); err != nil {
		t.Fatal(err)
	}
	// Disposable identity/session avoids account registration or real credentials.
	hash := sha256.Sum256([]byte("fixture-token"))
	productSQL(t, m, version, fmt.Sprintf(`
INSERT INTO users (id,username,display_name,role,status,created_at,updated_at) VALUES ('agent-fixture','agent-fixture','fixture','admin','active',now(),now());
INSERT INTO auth_sessions (id,user_id,token_hash,expires_at,created_at,updated_at) VALUES ('fixture-session','agent-fixture','%s',now()+interval '1 hour',now(),now());
INSERT INTO credit_accounts (user_id,available_microcredits,reserved_microcredits,version,created_at,updated_at) VALUES ('agent-fixture',1000000,0,1,now(),now());
INSERT INTO canvas_projects (id,user_id,title,payload_json,revision,created_at,updated_at) VALUES ('agent-canvas','agent-fixture','fixture','{"nodes":[]}',1,now(),now());
CREATE TABLE updater_evidence (marker text PRIMARY KEY); INSERT INTO updater_evidence VALUES ('initial');`, hex.EncodeToString(hash[:])))
	channelData := productAgentAPI(t, m, version, "POST", "/api/admin/channels", map[string]any{
		"name": "isolated model", "baseUrl": "http://model-fixture:18080/v1", "apiKey": "test-only", "models": []string{}, "enabled": true,
	})
	var channel struct {
		Channel struct {
			ID string `json:"id"`
		} `json:"channel"`
	}
	if err := json.Unmarshal(channelData, &channel); err != nil || channel.Channel.ID == "" {
		t.Fatalf("fixture channel missing: %s (%v)", channelData, err)
	}
	productAgentAPI(t, m, version, "POST", "/api/admin/channels/"+channel.Channel.ID+"/models", map[string]any{
		"modelKey": "text-fixture", "displayName": "fixture text", "capability": "text", "protocol": "chat-completion", "enabled": true,
		"capabilityConfig": map[string]any{"version": 1, "text": map[string]any{
			"streaming": true, "contextWindowTokens": 128000, "maxOutputTokens": 16384,
			"references": map[string]any{"promptMaxChars": 100000},
		}},
		"priceTiers": []any{map[string]any{"billingMode": "fixed_request", "unitPriceMicrocredits": 100, "priceConfigured": true, "enabled": true}},
	})
	runData := productAgentAPI(t, m, version, "POST", "/api/agent/runs", map[string]any{
		"canvasId": "agent-canvas", "prompt": "Return a brief final answer without using tools.", "model": "text-fixture", "channelId": channel.Channel.ID, "channelModelKey": "text-fixture",
		"permissionMode": "read_only", "contextScope": []string{"canvas"}, "budget": map[string]any{"maxCredits": 1, "maxSteps": 1}, "idempotencyKey": "fixture-active-drain",
	})
	var run struct {
		Run struct {
			ID string `json:"id"`
		} `json:"run"`
	}
	if err := json.Unmarshal(runData, &run); err != nil || run.Run.ID == "" {
		t.Fatalf("fixture run missing: %s (%v)", runData, err)
	}
	deadline := time.Now().Add(60 * time.Second)
	for {
		count := productCompose(t, m, version, "exec", "-T", "yingce-agent", "node", "-e", `fetch('http://model-fixture:18080/status').then(r=>r.text()).then(console.log)`)
		if strings.Contains(count, `"calls":1`) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("real Agent did not reach held model request: %s; run=%s", count, productSQL(t, m, version, "SELECT status || ':' || coalesce(failure_message,'') FROM cloud_agent_executions"))
		}
		time.Sleep(200 * time.Millisecond)
	}
	active := productCompose(t, m, version, "exec", "-T", "yingce-agent", "node", "-e", `fetch('http://127.0.0.1:8081/healthz').then(r=>r.json()).then(s=>console.log(s.active))`)
	if strings.TrimSpace(active) != "1" {
		t.Fatalf("expected an active remote Agent, got %s", active)
	}
	t.Log("published remote Agent active=1; actual model request held before update")
	var stoppedState string
	runner := &productContainerRunner{execRunner: execRunner{dir: dir}, manager: m, version: version, fault: "migration"}
	runner.afterStop = func() error {
		var output bytes.Buffer
		sql := "SELECT e.status || ':' || (SELECT count(*) FROM tasks WHERE user_id='agent-fixture' AND operation='cloud_agent_step' AND status='succeeded') || ':' || a.available_microcredits || ':' || a.reserved_microcredits || ':' || (SELECT count(*) FROM cloud_agent_pi_sessions WHERE user_id='agent-fixture' AND session_jsonl LIKE '%fixture final answer%') FROM cloud_agent_executions e JOIN credit_accounts a ON a.user_id=e.user_id WHERE e.user_id='agent-fixture'"
		if err := m.compose(m.composePath(), version, time.Minute, &output, "exec", "-T", "postgres", "psql", "-U", "canvas", "-d", "canvas", "-Atc", sql); err != nil {
			return err
		}
		stoppedState = strings.TrimSpace(output.String())
		if stoppedState != "completed:1:999900:0:1" {
			return fmt.Errorf("Agent result, settlement or final session not committed before final backup: %s", stoppedState)
		}
		return nil
	}
	m.runner = runner
	// Release the in-flight response only after the backend listener closes.
	// The fixture upstream stays alive while the updater stops business services.
	released := make(chan error, 1)
	go func() {
		var output bytes.Buffer
		script := `const until=Date.now()+60000;(async()=>{while(Date.now()<until){let open=true;try{await fetch('http://backend:8080/api/health/ready',{signal:AbortSignal.timeout(1000)})}catch{open=false}if(!open){const h=await fetch('http://127.0.0.1:8081/healthz').then(r=>r.json());if(h.active!==1)throw Error('Agent stopped before active task drain');await fetch('http://model-fixture:18080/release');console.log('released during backend drain; Agent active=1');return}await new Promise(r=>setTimeout(r,100))}throw Error('backend did not begin shutdown')})().catch(e=>{console.error(e.message);process.exit(1)})`
		err := m.compose(m.composePath(), version, 70*time.Second, &output, "exec", "-T", "yingce-agent", "node", "-e", script)
		if err == nil && !strings.Contains(output.String(), "released during backend drain") {
			err = fmt.Errorf("release evidence missing: %s", output.String())
		}
		released <- err
	}()
	m.runUpdate(version, version)
	if err := <-released; err != nil {
		t.Fatal(err)
	}
	if m.state.Operation.Phase != PhaseRolledBack || !runner.graceful || !runner.observedFailure {
		t.Fatalf("active-Agent rollback incomplete: phase=%s graceful=%v migrationFailure=%v error=%s", m.state.Operation.Phase, runner.graceful, runner.observedFailure, m.state.Operation.Error)
	}
	deadline = time.Now().Add(60 * time.Second)
	for {
		status := productSQL(t, m, version, "SELECT status FROM cloud_agent_executions WHERE user_id='agent-fixture'")
		if status == "completed\n" {
			break
		}
		if strings.TrimSpace(status) == "failed" || time.Now().After(deadline) {
			t.Fatalf("Agent did not resume to completion: %s", productSQL(t, m, version, "SELECT status || ':' || coalesce(failure_message,'') FROM cloud_agent_executions"))
		}
		time.Sleep(250 * time.Millisecond)
	}
	if got := productSQL(t, m, version, "SELECT count(*) FROM tasks WHERE user_id='agent-fixture' AND operation='cloud_agent_step' AND status='succeeded'"); got != "1\n" {
		t.Fatalf("model task duplicated or not committed: %q", got)
	}
	if got := productSQL(t, m, version, "SELECT available_microcredits || ':' || reserved_microcredits FROM credit_accounts WHERE user_id='agent-fixture'"); got != "999900:0\n" {
		t.Fatalf("active task settlement duplicated/lost: %q", got)
	}
	if got := productSQL(t, m, version, "SELECT count(*) FROM cloud_agent_pi_sessions WHERE user_id='agent-fixture' AND session_jsonl LIKE '%fixture final answer%'"); got != "1\n" {
		t.Fatalf("Agent final session snapshot missing: %q", got)
	}
	calls := productCompose(t, m, version, "exec", "-T", "yingce-agent", "node", "-e", `fetch('http://model-fixture:18080/status').then(r=>r.text()).then(console.log)`)
	if !strings.Contains(calls, `"calls":1`) || !strings.Contains(calls, `"released":true`) {
		t.Fatalf("upstream called again after rollback: %s", calls)
	}
	t.Logf("stopped before final backup: %s; rollback: one model call, one 100-microcredit settlement, final session snapshot and completed run", stoppedState)
}

func productAgentAPI(t *testing.T, m *Manager, version, method, path string, body any) json.RawMessage {
	t.Helper()
	request, err := json.Marshal(map[string]any{"method": method, "headers": map[string]string{"Content-Type": "application/json", "Cookie": "open_ai_canvas_session=fixture-session.fixture-token"}, "body": body})
	if err != nil {
		t.Fatal(err)
	}
	// JSON is a JavaScript literal passed as an exec argument, never shell text.
	script := fmt.Sprintf(`const q=%s;q.body=JSON.stringify(q.body);fetch(%q,q).then(async r=>console.log(JSON.stringify({status:r.status,body:await r.text()}))).catch(e=>{console.error(e.message);process.exit(1)})`, request, "http://backend:8080"+path)
	output := productCompose(t, m, version, "exec", "-T", "yingce-agent", "node", "-e", script)
	var response struct {
		Status int    `json:"status"`
		Body   string `json:"body"`
	}
	if err := json.Unmarshal([]byte(output), &response); err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		Code int             `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal([]byte(response.Body), &envelope); err != nil || response.Status != 200 || envelope.Code != 0 {
		t.Fatalf("fixture API %s %s status=%d body=%s decode=%v", method, path, response.Status, response.Body, err)
	}
	return envelope.Data
}

const productAgentUpstream = `
const {createServer}=require('node:http');let calls=0,released=false;const waiting=[];
const answer=(res,body)=>{if(body.stream){res.writeHead(200,{'content-type':'text/event-stream'});res.write('data: '+JSON.stringify({id:'fixture',choices:[{index:0,delta:{role:'assistant',content:'fixture final answer'},finish_reason:null}]})+'\n\n');res.write('data: '+JSON.stringify({id:'fixture',choices:[{index:0,delta:{},finish_reason:'stop'}],usage:{prompt_tokens:10,completion_tokens:4,total_tokens:14}})+'\n\n');res.end('data: [DONE]\n\n')}else{res.writeHead(200,{'content-type':'application/json'});res.end(JSON.stringify({id:'fixture',choices:[{index:0,message:{role:'assistant',content:'fixture final answer'},finish_reason:'stop'}],usage:{prompt_tokens:10,completion_tokens:4,total_tokens:14}}))}};
createServer(async(req,res)=>{if(req.url==='/status'){res.end(JSON.stringify({calls,released}));return}if(req.url==='/release'){released=true;for(const [r,b] of waiting.splice(0))answer(r,b);res.end('released');return}if(req.method!=='POST'){res.writeHead(404);res.end();return}let text='';for await(const chunk of req)text+=chunk;const body=JSON.parse(text);calls++;if(released)answer(res,body);else waiting.push([res,body])}).listen(18080,'0.0.0.0');`
