// Package httputil provides shared HTTP client, JSON request, and response
// validation helpers.
//
// # What this package does
//
// 이 패키지는 서비스 간 HTTP 호출과 외부 API 호출에서 반복되는 client timeout,
// connection pool, HTTP/2 정책을 TransportProfile로 맞춥니다. 기본 transport의
// proxy, keep-alive, TLS 기본값은 유지하고 호출 목적에 맞는 profile만 주입합니다.
//
// JSON request 생성, API key header 적용, response body decode/discard, non-2xx
// response를 APIError로 변환하는 흐름도 이 패키지에서 제공합니다. 호출부는 error
// helper로 HTTP status와 API error code를 분기할 수 있습니다.
//
// # 외부 surface (public API)
//
//   - TransportProfile: timeout, pool, HTTP/2 정책을 담는 client profile입니다.
//   - NewClient: timeout만 지정한 단순 http.Client를 생성합니다.
//   - NewProfiledClient: TransportProfile을 적용한 http.Client를 생성하는 기본 진입점입니다.
//   - NewExternalAPIClient, NewInternalServiceClient, DefaultClient: 목적별 표준 profile client를 생성합니다.
//   - JSONClient, NewJSONClient: 내부 서비스 JSON API 호출용 client wrapper입니다.
//   - (*JSONClient).NewRequest, (*JSONClient).NewJSONRequest: API key와 JSON header를 적용한 request를 생성합니다.
//   - (*JSONClient).Do, (*JSONClient).CheckStatus, (*JSONClient).DecodeJSON, (*JSONClient).DiscardBody: 요청 실행과 response 처리를 위임합니다.
//   - CheckStatus: non-2xx response를 APIError로 변환합니다.
//   - DecodeJSON: response body를 decode하고 닫습니다.
//   - APIError, AsAPIError, IsStatus, IsCode: API error unwrap과 분기 helper입니다.
//
// # 주요 사용 패턴
//
//	client := httputil.NewExternalAPIClient(30 * time.Second)
//	resp, err := client.Get(url)
//	if err != nil {
//	    return err
//	}
//	if err := httputil.CheckStatus(resp); err != nil {
//	    return err
//	}
//
//	api := httputil.NewJSONClient(baseURL, apiKey, 10*time.Second)
//	req, err := api.NewJSONRequest(ctx, http.MethodPost, "/v1/jobs", payload)
//	if err != nil {
//	    return err
//	}
//	resp, err := api.Do(req)
//
// # 내부 helper 정책
//
// applyTransportProfile, baseProfiledTransport, external/internal profile 값,
// newAPIError, errorResponse, applyAPIKey는 패키지 내부 composition 전용입니다.
// 호출부는 transport helper를 직접 재구성하지 않고 NewProfiledClient 또는 목적별
// factory를 사용합니다.
package httputil
