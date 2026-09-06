package zoraxy_plugin

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

/*
	Dynamic Path Handler for Zoraxy Plugins
*/

type SniffResult int

const (
	SniffResultAccept SniffResult = iota // Forward the request to this plugin dynamic capture ingress
	SniffResultSkip                      // Skip this plugin and let next plugin / default router handle the request
)

type SniffHandler func(*DynamicSniffForwardRequest) SniffResult

type PathRouter struct {
	enableDebugPrint bool
}

func NewPathRouter() *PathRouter {
	return &PathRouter{
		enableDebugPrint: false,
	}
}

func (p *PathRouter) SetDebugPrintMode(enabled bool) {
	p.enableDebugPrint = enabled
}

func RegisterDynamicSniffHandler(mux *http.ServeMux, handler SniffHandler) {
	router := NewPathRouter()
	router.RegisterDynamicSniffHandler("/d_sniff/", mux, handler)
}

func RegisterDynamicCaptureHandler(mux *http.ServeMux, handler func(http.ResponseWriter, *http.Request)) {
	router := NewPathRouter()
	router.RegisterDynamicCaptureHandle("/d_capture/", mux, handler)
}

func (p *PathRouter) RegisterDynamicSniffHandler(sniffIngress string, mux *http.ServeMux, handler SniffHandler) {
	if !strings.HasSuffix(sniffIngress, "/") {
		sniffIngress = sniffIngress + "/"
	}
	mux.Handle(sniffIngress, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if p.enableDebugPrint {
			fmt.Println("[Dynamic Router] Request captured by sniff path: " + r.RequestURI)
		}

		jsonBytes, err := io.ReadAll(r.Body)
		if err != nil {
			if p.enableDebugPrint {
				fmt.Println("[Dynamic Router] Error reading request body:", err)
			}
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		payload, err := DecodeForwardRequestPayload(jsonBytes)
		if err != nil {
			if p.enableDebugPrint {
				fmt.Println("[Dynamic Router] Error decoding request payload:", err)
			}
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		forwardUUID := r.Header.Get("X-Zoraxy-RequestID")
		payload.requestUUID = forwardUUID
		payload.rawRequest = r

		sniffResult := handler(&payload)
		if sniffResult == SniffResultAccept {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("OK"))
		} else {
			w.WriteHeader(http.StatusNotImplemented)
			_, _ = w.Write([]byte("SKIP"))
		}
	}))
}

func (p *PathRouter) RegisterDynamicCaptureHandle(captureIngress string, mux *http.ServeMux, handlefunc func(http.ResponseWriter, *http.Request)) {
	if !strings.HasSuffix(captureIngress, "/") {
		captureIngress = captureIngress + "/"
	}
	prefixWithoutSlash := strings.TrimSuffix(captureIngress, "/")

	mux.Handle(captureIngress, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if p.enableDebugPrint {
			fmt.Println("[Dynamic Router] Request captured by ingress path: " + r.RequestURI)
		}

		cleanPath := r.URL.Path
		cleanPath = strings.TrimPrefix(cleanPath, captureIngress)
		cleanPath = strings.TrimPrefix(cleanPath, prefixWithoutSlash)
		if cleanPath == "" {
			cleanPath = "/"
		}
		if !strings.HasPrefix(cleanPath, "/") {
			cleanPath = "/" + cleanPath
		}
		r.URL.Path = cleanPath
		r.RequestURI = cleanPath
		if r.URL.RawQuery != "" {
			r.RequestURI += "?" + r.URL.RawQuery
		}

		handlefunc(w, r)
	}))
}

type DynamicSniffForwardRequest struct {
	Method     string              `json:"method"`
	Hostname   string              `json:"hostname"`
	URL        string              `json:"url"`
	Header     map[string][]string `json:"header"`
	RemoteAddr string              `json:"remote_addr"`
	Host       string              `json:"host"`
	RequestURI string              `json:"request_uri"`
	Proto      string              `json:"proto"`
	ProtoMajor int                 `json:"proto_major"`
	ProtoMinor int                 `json:"proto_minor"`

	rawRequest  *http.Request
	requestUUID string
}

func EncodeForwardRequestPayload(r *http.Request) DynamicSniffForwardRequest {
	return DynamicSniffForwardRequest{
		Method:     r.Method,
		Hostname:   r.Host,
		URL:        r.URL.String(),
		Header:     r.Header,
		RemoteAddr: r.RemoteAddr,
		Host:       r.Host,
		RequestURI: r.RequestURI,
		Proto:      r.Proto,
		ProtoMajor: r.ProtoMajor,
		ProtoMinor: r.ProtoMinor,
		rawRequest: r,
	}
}

func DecodeForwardRequestPayload(jsonBytes []byte) (DynamicSniffForwardRequest, error) {
	var payload DynamicSniffForwardRequest
	err := json.Unmarshal(jsonBytes, &payload)
	if err != nil {
		return DynamicSniffForwardRequest{}, err
	}
	return payload, nil
}

func (dsfr *DynamicSniffForwardRequest) GetRequest() *http.Request {
	return dsfr.rawRequest
}

func (dsfr *DynamicSniffForwardRequest) GetRequestUUID() string {
	return dsfr.requestUUID
}
