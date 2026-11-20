package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

type stubIDTokenVerifier struct {
	identity VerifiedToken
	err      error
}

func (v stubIDTokenVerifier) VerifyIDToken(context.Context, string) (VerifiedToken, error) {
	return v.identity, v.err
}

func TestFirebaseIDTokenMiddlewareRejectsMissingBearerToken(t *testing.T) {
	handler := firebaseIDTokenMiddleware(stubIDTokenVerifier{})(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("handler must not be called without a bearer token")
	}))

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/messages", nil))

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
}

func TestFirebaseIDTokenMiddlewareRejectsInvalidToken(t *testing.T) {
	handler := firebaseIDTokenMiddleware(stubIDTokenVerifier{err: errors.New("invalid or expired token")})(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("handler must not be called for an invalid token")
	}))

	request := httptest.NewRequest(http.MethodGet, "/api/messages", nil)
	request.Header.Set("Authorization", "Bearer invalid-token")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
}

func TestFirebaseIDTokenMiddlewareRejectsUnverifiedEmail(t *testing.T) {
	verifier := stubIDTokenVerifier{identity: VerifiedToken{
		UID: "user-123", Email: "user@example.com", EmailVerified: false,
	}}
	handler := firebaseIDTokenMiddleware(verifier)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("handler must not be called when email is unverified")
	}))

	request := httptest.NewRequest(http.MethodGet, "/api/messages", nil)
	request.Header.Set("Authorization", "Bearer valid-signature-unverified-email")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
}

func TestFirebaseIDTokenMiddlewarePassesVerifiedIdentityToHandler(t *testing.T) {
	want := VerifiedToken{UID: "user-123", Email: "user@example.com", EmailVerified: true}
	handler := firebaseIDTokenMiddleware(stubIDTokenVerifier{identity: want})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, ok := UserIdentityFromContext(r.Context())
		if !ok {
			t.Fatal("verified identity missing from request context")
		}
		if got != want {
			t.Fatalf("identity = %#v, want %#v", got, want)
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	request := httptest.NewRequest(http.MethodGet, "/api/messages", nil)
	request.Header.Set("Authorization", "Bearer valid-token")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNoContent)
	}
}

func TestHTTPHandlerKeepsHealthPublicAndAllowsCORSPreflight(t *testing.T) {
	s := &chatServer{}
	handler := newHTTPHandler(s, stubIDTokenVerifier{err: errors.New("must not be called")})

	health := httptest.NewRecorder()
	handler.ServeHTTP(health, httptest.NewRequest(http.MethodGet, "/health", nil))
	if health.Code != http.StatusOK {
		t.Fatalf("health status = %d, want %d", health.Code, http.StatusOK)
	}

	root := httptest.NewRecorder()
	handler.ServeHTTP(root, httptest.NewRequest(http.MethodGet, "/", nil))
	if root.Code != http.StatusOK {
		t.Fatalf("root status = %d, want %d", root.Code, http.StatusOK)
	}

	preflight := httptest.NewRequest(http.MethodOptions, "/api/messages", nil)
	preflight.Header.Set("Origin", "https://example.com")
	preflight.Header.Set("Access-Control-Request-Method", "POST")
	preflight.Header.Set("Access-Control-Request-Headers", "authorization,content-type")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, preflight)
	if response.Code != http.StatusNoContent {
		t.Fatalf("preflight status = %d, want %d", response.Code, http.StatusNoContent)
	}
	if got := response.Header().Get("Access-Control-Allow-Headers"); got != "Authorization, Content-Type" {
		t.Fatalf("Access-Control-Allow-Headers = %q, want Authorization, Content-Type", got)
	}
}

func TestHTTPHandlerProtectsAPIRoutes(t *testing.T) {
	handler := newHTTPHandler(&chatServer{}, stubIDTokenVerifier{err: errors.New("invalid token")})
	for _, path := range []string{
		"/api/servers",
		"/api/rooms",
		"/api/messages",
		"/api/me",
		"/api/users",
		"/api/dms",
		"/api/push/register",
		"/api/calls/signals",
	} {
		t.Run(path, func(t *testing.T) {
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
			if response.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
			}
		})
	}
}
