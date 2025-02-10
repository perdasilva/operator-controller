package index

import (
	"bytes"
	"fmt"
	"io"
	"k8s.io/client-go/kubernetes"
	"net/http"
	"net/url"
	"strings"
)

func NewCatalogServiceRoundTripper(cl *kubernetes.Clientset) http.RoundTripper {
	return &proxyRoundTripper{
		clientset: cl,
	}
}

type proxyRoundTripper struct {
	clientset *kubernetes.Clientset
}

func parseHost(host string) (string, string, error) {
	tokens := strings.Split(host, ".")
	if len(tokens) < 2 {
		return "", "", fmt.Errorf("invalid host: %s", host)
	}
	return tokens[0], tokens[1], nil
}

func flattenQueryParams(queryParams url.Values) map[string]string {
	out := make(map[string]string, len(queryParams))
	for k, vs := range queryParams {
		out[k] = strings.Join(vs, ",")
	}
	return out
}

func (prt *proxyRoundTripper) proxyRequest(req *http.Request) ([]byte, error) {
	if req.Method != http.MethodGet {
		return nil, fmt.Errorf("'%s' method not allowed", req.Method)
	}
	serviceName, namespace, err := parseHost(req.Host)
	if err != nil {
		return nil, fmt.Errorf("failed to determine service name and namespace from host: %v", err)
	}
	proxyReq := prt.clientset.CoreV1().
		Services(namespace).
		ProxyGet(req.URL.Scheme, serviceName, req.URL.Port(), req.URL.Path, flattenQueryParams(req.URL.Query()))
	return proxyReq.DoRaw(req.Context())
}

func (prt *proxyRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	bodyBytes, err := prt.proxyRequest(req)
	if err != nil {
		return nil, err
	}
	fmt.Println(string(bodyBytes))
	resp := &http.Response{
		Status:     "200 OK",
		StatusCode: 200,
		Body:       io.NopCloser(bytes.NewReader(bodyBytes)),
		Request:    req,
		Header:     make(http.Header),
	}
	return resp, nil
}
