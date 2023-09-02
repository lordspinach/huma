package humafiber

import (
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/gofiber/fiber/v2"
)

type fiberCtx struct {
	op   *huma.Operation
	orig *fiber.Ctx
}

func (ctx *fiberCtx) Operation() *huma.Operation {
	return ctx.op
}

func (ctx *fiberCtx) Matched() string {
	return ctx.orig.Route().Path
}

func (ctx *fiberCtx) Context() context.Context {
	return ctx.orig.Context()
}

func (ctx *fiberCtx) Method() string {
	return ctx.orig.Method()
}

func (ctx *fiberCtx) Host() string {
	return ctx.orig.Hostname()
}

func (ctx *fiberCtx) URL() url.URL {
	u, _ := url.Parse(string(ctx.orig.Request().RequestURI()))
	return *u
}

func (ctx *fiberCtx) Param(name string) string {
	return ctx.orig.Params(name)
}

func (ctx *fiberCtx) Query(name string) string {
	return ctx.orig.Query(name)
}

func (ctx *fiberCtx) Header(name string) string {
	return ctx.orig.Get(name)
}

func (ctx *fiberCtx) EachHeader(cb func(name, value string)) {
	ctx.orig.Request().Header.VisitAll(func(k, v []byte) {
		cb(string(k), string(v))
	})
}

func (ctx *fiberCtx) BodyReader() io.Reader {
	return ctx.orig.Request().BodyStream()
}

func (ctx *fiberCtx) GetMultipartForm() (*multipart.Form, error) {
	return ctx.orig.MultipartForm()
}

func (ctx *fiberCtx) SetReadDeadline(deadline time.Time) error {
	// Note: for this to work properly you need to do two things:
	// 1. Set the Fiber app's `StreamRequestBody` to `true`
	// 2. Set the Fiber app's `BodyLimit` to some small value like `1`
	// Fiber will only call the request handler for streaming once the limit is
	// reached. This is annoying but currently how things work.
	return ctx.orig.Context().Conn().SetReadDeadline(deadline)
}

func (ctx *fiberCtx) SetStatus(code int) {
	ctx.orig.Status(code)
}

func (ctx *fiberCtx) AppendHeader(name string, value string) {
	ctx.orig.Append(name, value)
}

func (ctx *fiberCtx) SetHeader(name string, value string) {
	ctx.orig.Set(name, value)
}

func (ctx *fiberCtx) BodyWriter() io.Writer {
	return ctx.orig
}

type fiberAdapter struct {
	router *fiber.App
}

func (a *fiberAdapter) Handle(op *huma.Operation, handler func(huma.Context)) {
	// Convert {param} to :param
	path := op.Path
	path = strings.ReplaceAll(path, "{", ":")
	path = strings.ReplaceAll(path, "}", "")
	a.router.Add(op.Method, path, func(c *fiber.Ctx) error {
		ctx := &fiberCtx{op: op, orig: c}
		handler(ctx)
		return nil
	})
}

func (a *fiberAdapter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// b, _ := httputil.DumpRequest(r, true)
	// fmt.Println(string(b))
	resp, err := a.router.Test(r)
	if err != nil {
		panic(err)
	}
	h := w.Header()
	for k, v := range resp.Header {
		for item := range v {
			h.Add(k, v[item])
		}
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

func New(r *fiber.App, config huma.Config) huma.API {
	return huma.NewAPI(config, &fiberAdapter{router: r})
}
