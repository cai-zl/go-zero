package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httputil"

	"zero/core/logx"
	"zero/ngin/internal"

	"github.com/dgrijalva/jwt-go"
)

const (
	jwtAudience    = "aud"
	jwtExpire      = "exp"
	jwtId          = "jti"
	jwtIssueAt     = "iat"
	jwtIssuer      = "iss"
	jwtNotBefore   = "nbf"
	jwtSubject     = "sub"
	noDetailReason = "no detail reason"
)

var (
	errInvalidToken = errors.New("invalid auth token")
	errNoClaims     = errors.New("no auth params")
)

type (
	AuthorizeOptions struct {
		PrevSecret string
		Callback   UnauthorizedCallback
	}

	UnauthorizedCallback func(w http.ResponseWriter, r *http.Request, err error)
	AuthorizeOption      func(opts *AuthorizeOptions)
)

func Authorize(secret string, opts ...AuthorizeOption) func(http.Handler) http.Handler {
	var authOpts AuthorizeOptions
	for _, opt := range opts {
		opt(&authOpts)
	}

	parser := internal.NewTokenParser()
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token, err := parser.ParseToken(r, secret, authOpts.PrevSecret)
			if err != nil {
				unauthorized(w, r, err, authOpts.Callback)
				return
			}

			if !token.Valid {
				unauthorized(w, r, errInvalidToken, authOpts.Callback)
				return
			}

			claims, ok := token.Claims.(jwt.MapClaims)
			if !ok {
				unauthorized(w, r, errNoClaims, authOpts.Callback)
				return
			}

			ctx := r.Context()
			for k, v := range claims {
				switch k {
				case jwtAudience, jwtExpire, jwtId, jwtIssueAt, jwtIssuer, jwtNotBefore, jwtSubject:
					// ignore the standard claims
				default:
					ctx = context.WithValue(ctx, k, v)
				}
			}

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func WithPrevSecret(secret string) AuthorizeOption {
	return func(opts *AuthorizeOptions) {
		opts.PrevSecret = secret
	}
}

func WithUnauthorizedCallback(callback UnauthorizedCallback) AuthorizeOption {
	return func(opts *AuthorizeOptions) {
		opts.Callback = callback
	}
}

func detailAuthLog(r *http.Request, reason string) {
	// discard dump error, only for debug purpose
	details, _ := httputil.DumpRequest(r, true)
	logx.Errorf("authorize failed: %s\n=> %+v", reason, string(details))
}

func unauthorized(w http.ResponseWriter, r *http.Request, err error, callback UnauthorizedCallback) {
	writer := newGuardedResponseWriter(w)

	if err != nil {
		detailAuthLog(r, err.Error())
	} else {
		detailAuthLog(r, noDetailReason)
	}
	if callback != nil {
		callback(writer, r, err)
	}

	writer.WriteHeader(http.StatusUnauthorized)
}

type guardedResponseWriter struct {
	writer      http.ResponseWriter
	wroteHeader bool
}

func newGuardedResponseWriter(w http.ResponseWriter) *guardedResponseWriter {
	return &guardedResponseWriter{
		writer: w,
	}
}

func (grw *guardedResponseWriter) Header() http.Header {
	return grw.writer.Header()
}

func (grw *guardedResponseWriter) Write(body []byte) (int, error) {
	return grw.writer.Write(body)
}

func (grw *guardedResponseWriter) WriteHeader(statusCode int) {
	if grw.wroteHeader {
		return
	}

	grw.wroteHeader = true
	grw.writer.WriteHeader(statusCode)
}
