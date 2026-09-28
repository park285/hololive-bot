package pobroker

import (
	"context"
	"encoding/json/v2"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"sync"
	"time"
	"unicode/utf8"
	"uuid"
)

const (
	protocolVersion    = 1
	operationLimit     = 8 * time.Second
	workerStartupLimit = 30 * time.Second
	requestLimit       = 1 << 20
	// HTTP IdleTimeout인 idleConnectionLimit은 client free-socket timeout(youtubejs/src/proof-broker.mjs, 1초)보다
	// 훨씬 길어야 합니다. Node event loop가 수 초 멈춰도 client가 먼저 유휴 연결을 닫아,
	// 서버가 이미 닫은 socket을 client가 재사용하다 EPIPE(broker_unavailable)를 받지 않습니다.
	idleConnectionLimit = 30 * time.Second
)

type State string

const (
	Idle              State = "IDLE"
	Starting          State = "STARTING"
	AwaitingChallenge State = "AWAITING_CHALLENGE"
	AwaitingIntegrity State = "AWAITING_INTEGRITY"
	Ready             State = "READY"
)

type Broker struct {
	generation      string
	revision        string
	node            string
	script          string
	server          *http.Server
	admission       chan struct{}
	healthAdmission chan struct{}
	serial          chan struct{}
	mu              sync.RWMutex
	state           State
	worker          *worker
	used            bool
	expires         time.Time
	leaseTimer      *time.Timer
	retiring        bool
	// exitReason은 처음 retiring을 표시한 호출의 원인이며 b.mu가 보호합니다.
	exitReason ExitReason
	retireOnce sync.Once
	// retireErr is written inside retireOnce and read only after it completes.
	retireErr error
}

type envelope struct {
	ProtocolVersion int    `json:"protocol_version"`
	Generation      string `json:"generation"`
}

type sessionRequest struct {
	envelope

	UserAgent string `json:"user_agent"`
}

type challengeRequest struct {
	envelope

	Program     string `json:"program"`
	GlobalName  string `json:"global_name"`
	Interpreter string `json:"interpreter"`
}

type activateRequest struct {
	envelope

	IntegrityToken string `json:"integrity_token"`
	ValidForMS     int64  `json:"valid_for_ms"`
}

type mintRequest struct {
	envelope

	VideoID string `json:"video_id"`
}

type workerPrepared struct {
	Type     string `json:"type"`
	Prepared bool   `json:"prepared"`
}

type workerSnapshot struct {
	Type     string `json:"type"`
	Snapshot string `json:"snapshot"`
}

type workerReady struct {
	Type  string `json:"type"`
	Ready bool   `json:"ready"`
}

type workerMint struct {
	Type    string `json:"type"`
	VideoID string `json:"video_id"`
	PoToken string `json:"po_token"`
}

func New(revision, node, script string) *Broker {
	return &Broker{
		generation: uuid.New().String(), revision: revision, node: node, script: script,
		state: Idle, admission: make(chan struct{}, 7), healthAdmission: make(chan struct{}, 1),
		serial: make(chan struct{}, 1),
	}
}

func (b *Broker) Generation() string { return b.generation }

// Serve owns the listener until retirement. The caller must leave the process
// after Serve returns; reusing this Broker could reuse a VM or generation.
// Normal retirement returns nil; an error reports unconfirmed VM or listener
// cleanup and never includes worker output.
func (b *Broker) Serve(listener net.Listener) error {
	// keep-alive를 유지해 정상 응답 직후 서버가 연결을 닫지 않습니다. 응답 직후의 서버
	// close가 client의 새 연결 첫 read와 겹치면 AppArmor unix 미디에이션 경쟁
	// (upstream b1aea2c19607 미적용 커널)으로 Oops가 납니다. 유휴 연결은 client
	// (youtubejs/src/proof-broker.mjs)가 idleConnectionLimit보다 훨씬 먼저 닫고, 퇴역 시에는
	// retire의 server.Close가 유휴·진행 중 연결을 모두 즉시 닫습니다.
	b.server = &http.Server{
		Handler:           http.HandlerFunc(b.handle),
		ReadHeaderTimeout: 2 * time.Second, ReadTimeout: operationLimit,
		WriteTimeout: operationLimit, IdleTimeout: idleConnectionLimit,
		MaxHeaderBytes: 8 << 10, ErrorLog: log.New(io.Discard, "", 0),
	}

	// 신뢰된 SDK의 cold import는 서비스 준비 단계가 소유한다. 요청의 8초
	// 예산에 VM 기동을 섞지 않고 loaded 확인 전에는 health도 제공하지 않는다.
	ctx, cancel := context.WithTimeout(context.Background(), workerStartupLimit)
	initErr := b.initializeWorker(ctx)

	cancel()

	if initErr != nil {
		b.retire(ExitStartupFailed)

		return errors.Join(initErr, b.retireErr, listener.Close())
	}

	err := b.server.Serve(listener)
	// ErrServerClosed이면 retire가 이미 원인을 기록했으므로 아래 원인은 쓰이지 않습니다.
	b.retire(ExitListenerFailed)

	if errors.Is(err, http.ErrServerClosed) {
		return b.retireErr
	}

	return errors.Join(err, b.retireErr)
}

func (b *Broker) handle(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	ctx, cancel := context.WithTimeout(r.Context(), operationLimit)

	defer cancel()

	r = r.WithContext(ctx)

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")

	slot := b.admit(r)
	if slot == nil {
		failure(w, http.StatusServiceUnavailable, "busy")

		return
	}

	defer func() { <-slot }()

	if r.URL.RawQuery != "" || r.URL.Fragment != "" {
		failure(w, http.StatusBadRequest, "invalid_request")

		return
	}

	if r.Method == http.MethodGet && r.URL.Path == "/health" {
		b.health(w, r)

		return
	}

	body := requestBody(r)
	if body == nil {
		failure(w, http.StatusBadRequest, "invalid_request")

		return
	}

	if !decodeRequest(w, r, body) {
		return
	}

	select {
	case b.serial <- struct{}{}:
		defer func() { <-b.serial }()
	case <-ctx.Done():
		failure(w, http.StatusGatewayTimeout, "worker_timeout")

		return
	}

	if !b.validateEnvelope(w, body) || !b.available(w) {
		return
	}

	if ctx.Err() != nil {
		failure(w, http.StatusGatewayTimeout, "worker_timeout")

		return
	}

	b.dispatch(ctx, w, body, started)
}

func (b *Broker) admit(r *http.Request) chan struct{} {
	slot := b.admission

	if r.Method == http.MethodGet && r.URL.Path == "/health" {
		slot = b.healthAdmission
	}

	select {
	case slot <- struct{}{}:
		return slot
	default:
		return nil
	}
}

func (b *Broker) health(w http.ResponseWriter, r *http.Request) {
	if r.ContentLength > 0 || r.TransferEncoding != nil {
		failure(w, http.StatusBadRequest, "invalid_request")

		return
	}

	b.mu.RLock()

	state, retiring := b.state, b.retiring
	b.mu.RUnlock()

	if retiring {
		failure(w, http.StatusServiceUnavailable, "worker_failed")

		return
	}

	success(w, struct {
		ProtocolVersion int    `json:"protocol_version"`
		Generation      string `json:"generation"`
		State           State  `json:"state"`
		Revision        string `json:"revision"`
	}{protocolVersion, b.generation, state, b.revision})
}

func requestBody(r *http.Request) any {
	switch {
	case r.URL.Path == "/v1/session" && r.Method == http.MethodDelete:
		return new(envelope)
	case r.URL.Path == "/v1/session" && r.Method == http.MethodPost:
		return new(sessionRequest)
	case r.URL.Path == "/v1/challenge" && r.Method == http.MethodPost:
		return new(challengeRequest)
	case r.URL.Path == "/v1/activate" && r.Method == http.MethodPost:
		return new(activateRequest)
	case r.URL.Path == "/v1/mint" && r.Method == http.MethodPost:
		return new(mintRequest)
	default:
		return nil
	}
}

func decodeRequest(w http.ResponseWriter, r *http.Request, body any) bool {
	if r.Header.Get("Content-Type") != "application/json" || r.ContentLength > requestLimit {
		failure(w, http.StatusBadRequest, "invalid_request")

		return false
	}

	if err := decodeBody(w, r, body); err != nil {
		if r.Context().Err() != nil {
			failure(w, http.StatusGatewayTimeout, "worker_timeout")
		} else {
			failure(w, http.StatusBadRequest, "invalid_request")
		}

		return false
	}

	return true
}

func (b *Broker) validateEnvelope(w http.ResponseWriter, body any) bool {
	var (
		version    int
		generation string
	)

	switch v := body.(type) {
	case *envelope:
		version, generation = v.ProtocolVersion, v.Generation
	case *sessionRequest:
		version, generation = v.ProtocolVersion, v.Generation
	case *challengeRequest:
		version, generation = v.ProtocolVersion, v.Generation
	case *activateRequest:
		version, generation = v.ProtocolVersion, v.Generation
	case *mintRequest:
		version, generation = v.ProtocolVersion, v.Generation
	}

	if version != protocolVersion || generation == "" {
		failure(w, http.StatusBadRequest, "invalid_request")

		return false
	}

	if generation != b.generation {
		failure(w, http.StatusConflict, "generation_mismatch")

		return false
	}

	return true
}

func (b *Broker) available(w http.ResponseWriter) bool {
	b.mu.RLock()

	retiring := b.retiring
	b.mu.RUnlock()

	if retiring {
		failure(w, http.StatusServiceUnavailable, "worker_failed")

		return false
	}

	return true
}

func (b *Broker) dispatch(ctx context.Context, w http.ResponseWriter, body any, started time.Time) {
	switch v := body.(type) {
	case *sessionRequest:
		b.session(ctx, w, v)
	case *challengeRequest:
		b.challenge(ctx, w, v)
	case *activateRequest:
		b.activate(ctx, w, v, started)
	case *mintRequest:
		b.mint(ctx, w, v)
	case *envelope:
		success(w, struct {
			ProtocolVersion int    `json:"protocol_version"`
			Generation      string `json:"generation"`
			Closed          bool   `json:"closed"`
		}{protocolVersion, b.generation, true})
		b.retireAfterResponse(w, ExitSessionClosed)
	}
}

func decodeBody(w http.ResponseWriter, r *http.Request, target any) error {
	defer r.Body.Close()

	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, requestLimit))
	if err != nil || len(data) == 0 {
		return errWorker
	}

	return json.Unmarshal(data, target, json.RejectUnknownMembers(true))
}

func validText(value string, limit int) bool {
	return value != "" && len(value) <= limit && utf8.ValidString(value)
}

func (b *Broker) reserveSession() *worker {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.worker == nil || b.state != Idle || b.used || b.retiring {
		return nil
	}

	b.used, b.state = true, Starting

	return b.worker
}

func validChallenge(v *challengeRequest) bool {
	return validText(v.Program, requestLimit) && validText(v.GlobalName, 1024) &&
		validText(v.Interpreter, requestLimit)
}

func (b *Broker) session(ctx context.Context, w http.ResponseWriter, v *sessionRequest) {
	if !validText(v.UserAgent, 1024) {
		failure(w, http.StatusBadRequest, "invalid_request")

		return
	}

	worker := b.reserveSession()
	if worker == nil {
		failure(w, http.StatusConflict, "invalid_state")

		return
	}

	var result workerPrepared

	err := worker.exchange(ctx, struct {
		Type      string `json:"type"`
		UserAgent string `json:"user_agent"`
	}{"prepare", v.UserAgent}, &result)

	if err != nil || result.Type != "prepared" || !result.Prepared {
		b.workerFailure(w, err)

		return
	}

	if ctx.Err() != nil {
		b.workerFailure(w, ctx.Err())

		return
	}

	select {
	case <-worker.done:
		b.workerFailure(w, errWorker)

		return
	default:
	}

	b.mu.Lock()

	if b.retiring {
		b.mu.Unlock()
		b.workerFailure(w, errWorker)

		return
	}

	b.setLeaseLocked(AwaitingChallenge, time.Now().Add(30*time.Second))
	b.mu.Unlock()

	if !success(w, struct {
		ProtocolVersion int    `json:"protocol_version"`
		Generation      string `json:"generation"`
		Prepared        bool   `json:"prepared"`
	}{protocolVersion, b.generation, true}) {
		failure(w, http.StatusServiceUnavailable, "worker_failed")
		b.retireAfterResponse(w, ExitResponseFailed)
	}
}

func (b *Broker) challenge(ctx context.Context, w http.ResponseWriter, v *challengeRequest) {
	if !validChallenge(v) {
		failure(w, http.StatusBadRequest, "invalid_request")

		return
	}

	worker := b.pendingWorker(w, AwaitingChallenge)
	if worker == nil {
		return
	}

	var result workerSnapshot

	err := worker.exchange(ctx, struct {
		Type        string `json:"type"`
		Program     string `json:"program"`
		GlobalName  string `json:"global_name"`
		Interpreter string `json:"interpreter"`
	}{"challenge", v.Program, v.GlobalName, v.Interpreter}, &result)

	if errors.Is(err, errBeforeDispatch) {
		failure(w, http.StatusGatewayTimeout, "worker_timeout")

		return
	}

	if err != nil || result.Type != "snapshot" || !validText(result.Snapshot, 60000) {
		b.workerFailure(w, err)

		return
	}

	if ctx.Err() != nil {
		b.workerFailure(w, ctx.Err())

		return
	}

	select {
	case <-worker.done:
		b.workerFailure(w, errWorker)

		return
	default:
	}

	b.mu.Lock()

	if b.retiring || !time.Now().Before(b.expires) {
		b.mu.Unlock()
		failure(w, http.StatusConflict, "expired")
		b.retireAfterResponse(w, ExitLeaseExpired)

		return
	}

	b.state = AwaitingIntegrity // prepare에서 시작한 만료 시한은 challenge 이후에도 유지합니다.
	b.mu.Unlock()

	if !success(w, struct {
		ProtocolVersion int    `json:"protocol_version"`
		Generation      string `json:"generation"`
		Snapshot        string `json:"snapshot"`
	}{protocolVersion, b.generation, result.Snapshot}) {
		failure(w, http.StatusServiceUnavailable, "worker_failed")
		b.retireAfterResponse(w, ExitResponseFailed)
	}
}

func (b *Broker) activate(ctx context.Context, w http.ResponseWriter, v *activateRequest, started time.Time) {
	if !validText(v.IntegrityToken, requestLimit) || v.ValidForMS < 1 || v.ValidForMS > 43200000 {
		failure(w, http.StatusBadRequest, "invalid_request")

		return
	}

	worker := b.pendingWorker(w, AwaitingIntegrity)
	if worker == nil {
		return
	}

	var result workerReady

	err := worker.exchange(ctx, struct {
		Type           string `json:"type"`
		IntegrityToken string `json:"integrity_token"`
	}{"activate", v.IntegrityToken}, &result)

	if err != nil || result.Type != "ready" || !result.Ready {
		b.workerFailure(w, err)

		return
	}

	if ctx.Err() != nil {
		b.workerFailure(w, ctx.Err())

		return
	}

	select {
	case <-worker.done:
		b.workerFailure(w, errWorker)

		return
	default:
	}

	deadline := started.Add(time.Duration(v.ValidForMS) * time.Millisecond)
	if !time.Now().Before(deadline) {
		failure(w, http.StatusConflict, "expired")
		b.retireAfterResponse(w, ExitLeaseExpired)

		return
	}

	b.mu.Lock()

	if b.retiring || !time.Now().Before(b.expires) {
		b.mu.Unlock()
		failure(w, http.StatusConflict, "expired")
		b.retireAfterResponse(w, ExitLeaseExpired)

		return
	}

	b.setLeaseLocked(Ready, deadline)
	b.mu.Unlock()

	if !success(w, struct {
		ProtocolVersion int    `json:"protocol_version"`
		Generation      string `json:"generation"`
		Ready           bool   `json:"ready"`
	}{protocolVersion, b.generation, true}) {
		b.retireAfterResponse(w, ExitResponseFailed)
	}
}

func (b *Broker) pendingWorker(w http.ResponseWriter, expected State) *worker {
	b.mu.RLock()

	state, worker, pendingExpiry, retiring := b.state, b.worker, b.expires, b.retiring
	b.mu.RUnlock()

	if state != expected || worker == nil || retiring {
		failure(w, http.StatusConflict, "invalid_state")

		return nil
	}

	if !time.Now().Before(pendingExpiry) {
		failure(w, http.StatusConflict, "expired")
		b.retireAfterResponse(w, ExitLeaseExpired)

		return nil
	}

	return worker
}

func (b *Broker) mint(ctx context.Context, w http.ResponseWriter, v *mintRequest) {
	if !validText(v.VideoID, 128) {
		failure(w, http.StatusBadRequest, "invalid_request")

		return
	}

	b.mu.RLock()

	state, expiry, worker := b.state, b.expires, b.worker
	b.mu.RUnlock()

	if state != Ready || worker == nil {
		failure(w, http.StatusConflict, "invalid_state")

		return
	}

	if !time.Now().Before(expiry) {
		failure(w, http.StatusConflict, "expired")
		b.retireAfterResponse(w, ExitLeaseExpired)

		return
	}

	var result workerMint

	err := worker.exchange(ctx, struct {
		Type    string `json:"type"`
		VideoID string `json:"video_id"`
	}{"mint", v.VideoID}, &result)

	if errors.Is(err, errBeforeDispatch) {
		failure(w, http.StatusGatewayTimeout, "worker_timeout")

		return
	}

	if err != nil || result.Type != "minted" || result.VideoID != v.VideoID || !validText(result.PoToken, 8192) {
		b.workerFailure(w, err)

		return
	}

	if !time.Now().Before(expiry) {
		failure(w, http.StatusConflict, "expired")
		b.retireAfterResponse(w, ExitLeaseExpired)

		return
	}

	if ctx.Err() != nil {
		b.workerFailure(w, ctx.Err())

		return
	}

	select {
	case <-worker.done:
		b.workerFailure(w, errWorker)

		return
	default:
	}

	if !success(w, struct {
		ProtocolVersion int    `json:"protocol_version"`
		Generation      string `json:"generation"`
		VideoID         string `json:"video_id"`
		PoToken         string `json:"po_token"`
	}{protocolVersion, b.generation, v.VideoID, result.PoToken}) {
		failure(w, http.StatusServiceUnavailable, "worker_failed")
		b.retireAfterResponse(w, ExitResponseFailed)
	}
}

func success(w http.ResponseWriter, body any) bool {
	encoded, err := json.Marshal(body)
	if err != nil || len(encoded) > maxWorkerFrame {
		return false
	}

	_, err = w.Write(encoded)

	return err == nil
}

func failure(w http.ResponseWriter, status int, code string) {
	w.WriteHeader(status)

	// The fixed body cannot fail to encode, so an error means the peer is gone.
	// net/http drops that connection, and every state-changing failure path
	// retires the generation whether or not this body was delivered.
	if err := json.MarshalWrite(w, struct {
		ProtocolVersion int `json:"protocol_version"`
		Error           struct {
			Code string `json:"code"`
		} `json:"error"`
	}{ProtocolVersion: protocolVersion, Error: struct {
		Code string `json:"code"`
	}{code}}); err != nil {
		return
	}
}
