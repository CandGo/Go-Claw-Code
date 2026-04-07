package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// SigV4 constants following AWS Signature Version 4 specification.
const (
	sigV4Algo      = "AWS4-HMAC-SHA256"
	sigV4Service   = "bedrock"
	sigV4ReqType   = "aws4_request"
	headerAmzDate  = "X-Amz-Date"
	headerSecToken = "X-Amz-Security-Token"
)

// SigV4Signer applies AWS Signature Version 4 to HTTP requests.
type SigV4Signer struct {
	keyID     string
	secretKey string
	token     string
	region    string
}

// NewSigV4Signer builds a signer from Bedrock credentials and a region.
func NewSigV4Signer(creds *BedrockCreds, region string) *SigV4Signer {
	if creds == nil {
		return &SigV4Signer{region: region}
	}
	return &SigV4Signer{
		keyID:     creds.AccessKeyID,
		secretKey: creds.SecretAccessKey,
		token:     creds.SessionToken,
		region:    region,
	}
}

// Sign computes and attaches SigV4 headers to req. No-ops when credentials are absent.
func (s *SigV4Signer) Sign(req *http.Request) {
	if s.keyID == "" || s.secretKey == "" {
		return
	}

	now := time.Now().UTC()
	amzDate := now.Format("20060102T150405Z")
	dateStamp := now.Format("20060102")

	req.Header.Set("Host", req.URL.Host)
	req.Header.Set(headerAmzDate, amzDate)
	if s.token != "" {
		req.Header.Set(headerSecToken, s.token)
	}

	canonical := s.buildCanonicalRequest(req, amzDate)
	strToSign := s.buildStringToSign(amzDate, dateStamp, canonical)
	signingKey := s.deriveSigningKey(dateStamp)
	signature := hex.EncodeToString(computeHMAC(signingKey, strToSign))

	signedHdrs := collectSignedHeaderNames(req)
	credScope := fmt.Sprintf("%s/%s/%s/%s", dateStamp, s.region, sigV4Service, sigV4ReqType)
	req.Header.Set("Authorization", fmt.Sprintf("%s Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		sigV4Algo, s.keyID, credScope, signedHdrs, signature))
}

// buildCanonicalRequest assembles the canonical request per the SigV4 spec.
func (s *SigV4Signer) buildCanonicalRequest(req *http.Request, amzDate string) string {
	uri := req.URL.EscapedPath()
	if uri == "" {
		uri = "/"
	}

	queryStr := encodeCanonicalQuery(req.URL.Query())
	hdrStr, _ := collectCanonicalHeaders(req)
	signedHdrs := collectSignedHeaderNames(req)
	payloadHash := hashHex("")

	return strings.Join([]string{
		req.Method,
		uri,
		queryStr,
		hdrStr,
		"",
		signedHdrs,
		payloadHash,
	}, "\n")
}

// buildStringToSign constructs the string-to-sign value.
func (s *SigV4Signer) buildStringToSign(amzDate, dateStamp, canonicalReq string) string {
	credScope := fmt.Sprintf("%s/%s/%s/%s", dateStamp, s.region, sigV4Service, sigV4ReqType)
	return strings.Join([]string{
		sigV4Algo,
		amzDate,
		credScope,
		hashHex(canonicalReq),
	}, "\n")
}

// deriveSigningKey follows the HMAC derivation chain: secret → date → region → service → request.
func (s *SigV4Signer) deriveSigningKey(dateStamp string) []byte {
	k := computeHMACRaw([]byte("AWS4"+s.secretKey), dateStamp)
	k = computeHMACRaw(k, s.region)
	k = computeHMACRaw(k, sigV4Service)
	k = computeHMACRaw(k, sigV4ReqType)
	return k
}

// encodeCanonicalQuery sorts and URL-encodes query parameters.
func encodeCanonicalQuery(q url.Values) string {
	if len(q) == 0 {
		return ""
	}
	keys := make([]string, 0, len(q))
	for k := range q {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var b strings.Builder
	for i, k := range keys {
		if i > 0 {
			b.WriteByte('&')
		}
		for j, v := range q[k] {
			if j > 0 {
				b.WriteByte('&')
			}
			b.WriteString(url.QueryEscape(k))
			b.WriteByte('=')
			b.WriteString(url.QueryEscape(v))
		}
	}
	return b.String()
}

// collectCanonicalHeaders builds the lowercase sorted header string required by SigV4.
func collectCanonicalHeaders(req *http.Request) (string, []string) {
	type hdrEntry struct {
		name  string
		value string
	}
	var entries []hdrEntry
	var names []string

	for k, vals := range req.Header {
		lk := strings.ToLower(k)
		if lk == "authorization" || lk == "user-agent" {
			continue
		}
		names = append(names, lk)
		for _, v := range vals {
			entries = append(entries, hdrEntry{lk, strings.TrimSpace(v)})
		}
	}
	sort.Strings(names)

	// Re-sort entries by name
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].name < entries[j].name
	})

	var b strings.Builder
	for _, e := range entries {
		b.WriteString(e.name)
		b.WriteByte(':')
		b.WriteString(e.value)
		b.WriteByte('\n')
	}
	return b.String(), names
}

// collectSignedHeaderNames returns the semicolon-separated list of signed header names.
func collectSignedHeaderNames(req *http.Request) string {
	var names []string
	for k := range req.Header {
		lk := strings.ToLower(k)
		if lk == "authorization" || lk == "user-agent" {
			continue
		}
		names = append(names, lk)
	}
	sort.Strings(names)
	return strings.Join(names, ";")
}

// hashHex returns the hex-encoded SHA-256 of data.
func hashHex(data string) string {
	h := sha256.Sum256([]byte(data))
	return hex.EncodeToString(h[:])
}

// computeHMAC returns the hex-encoded HMAC-SHA256 of data using key.
func computeHMAC(key []byte, data string) []byte {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(data))
	return h.Sum(nil)
}

// computeHMACRaw returns raw HMAC-SHA256 bytes.
func computeHMACRaw(key []byte, data string) []byte {
	return computeHMAC(key, data)
}
