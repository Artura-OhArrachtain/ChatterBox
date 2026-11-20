package main

import (
	"context"
	"net/http"
	"strings"

	firebaseauth "firebase.google.com/go/v4/auth"
)

// VerifiedToken contains the identity claims needed by the API after Firebase
// has verified the ID token's signature, audience, and expiration.
type VerifiedToken struct {
	UID           string
	Email         string
	EmailVerified bool
}

// IDTokenVerifier keeps request authentication independent of Firebase so
// middleware tests do not need credentials or a Firebase emulator.
type IDTokenVerifier interface {
	VerifyIDToken(context.Context, string) (VerifiedToken, error)
}

type firebaseIDTokenVerifier struct {
	client *firebaseauth.Client
}

func (v firebaseIDTokenVerifier) VerifyIDToken(ctx context.Context, idToken string) (VerifiedToken, error) {
	token, err := v.client.VerifyIDToken(ctx, idToken)
	if err != nil {
		return VerifiedToken{}, err
	}
	email, _ := token.Claims["email"].(string)
	emailVerified, _ := token.Claims["email_verified"].(bool)
	return VerifiedToken{
		UID:           token.UID,
		Email:         email,
		EmailVerified: emailVerified,
	}, nil
}

type identityContextKey struct{}

// UserIdentityFromContext returns the authenticated Firebase identity on a
// request protected by firebaseIDTokenMiddleware.
func UserIdentityFromContext(ctx context.Context) (VerifiedToken, bool) {
	identity, ok := ctx.Value(identityContextKey{}).(VerifiedToken)
	return identity, ok
}

func firebaseIDTokenMiddleware(verifier IDTokenVerifier) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodOptions {
				next.ServeHTTP(w, r)
				return
			}

			scheme, idToken, found := strings.Cut(strings.TrimSpace(r.Header.Get("Authorization")), " ")
			idToken = strings.TrimSpace(idToken)
			if !found || !strings.EqualFold(scheme, "Bearer") || idToken == "" || strings.ContainsAny(idToken, " \t\r\n") {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}

			identity, err := verifier.VerifyIDToken(r.Context(), idToken)
			if err != nil || !identity.EmailVerified || strings.TrimSpace(identity.UID) == "" || strings.TrimSpace(identity.Email) == "" {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}

			ctx := context.WithValue(r.Context(), identityContextKey{}, identity)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
