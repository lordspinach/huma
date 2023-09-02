package humagin

import (
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/gin-gonic/gin"
)

type ginCtx struct {
	op   *huma.Operation
	orig *gin.Context
}

func (ctx *ginCtx) Request() *http.Request {
	return ctx.orig.Request
}

func (ctx *ginCtx) Operation() *huma.Operation {
	return ctx.op
}

func (ctx *ginCtx) Context() context.Context {
	return ctx.orig.Request.Context()
}

func (ctx *ginCtx) Method() string {
	return ctx.orig.Request.Method
}

func (ctx *ginCtx) Host() string {
	return ctx.orig.Request.Host
}

func (ctx *ginCtx) URL() url.URL {
	return *ctx.orig.Request.URL
}

func (ctx *ginCtx) Param(name string) string {
	return ctx.orig.Param(name)
}

func (ctx *ginCtx) Query(name string) string {
	return ctx.orig.Query(name)
}

func (ctx *ginCtx) Header(name string) string {
	return ctx.orig.GetHeader(name)
}

func (ctx *ginCtx) EachHeader(cb func(name, value string)) {
	for name, values := range ctx.orig.Request.Header {
		for _, value := range values {
			cb(name, value)
		}
	}
}

func (ctx *ginCtx) BodyReader() io.Reader {
	return ctx.orig.Request.Body
}

func (ctx *ginCtx) GetMultipartForm() (*multipart.Form, error) {
	err := ctx.orig.Request.ParseMultipartForm(8 * 1024)
	return ctx.orig.Request.MultipartForm, err
}

func (ctx *ginCtx) SetReadDeadline(deadline time.Time) error {
	return huma.SetReadDeadline(ctx.orig.Writer, deadline)
}

func (ctx *ginCtx) SetStatus(code int) {
	ctx.orig.Status(code)
}

func (ctx *ginCtx) AppendHeader(name string, value string) {
	ctx.orig.Writer.Header().Add(name, value)
}

func (ctx *ginCtx) SetHeader(name string, value string) {
	ctx.orig.Header(name, value)
}

func (ctx *ginCtx) BodyWriter() io.Writer {
	return ctx.orig.Writer
}

type ginAdapter struct {
	router *gin.Engine
}

func (a *ginAdapter) Handle(op *huma.Operation, handler func(huma.Context)) {
	// Convert {param} to :param
	path := op.Path
	path = strings.ReplaceAll(path, "{", ":")
	path = strings.ReplaceAll(path, "}", "")
	a.router.Handle(op.Method, path, func(c *gin.Context) {
		ctx := &ginCtx{op: op, orig: c}
		handler(ctx)
	})
}

func (a *ginAdapter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	a.router.ServeHTTP(w, r)
}

func New(r *gin.Engine, config huma.Config) huma.API {
	return huma.NewAPI(config, &ginAdapter{router: r})
}
