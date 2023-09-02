package huma

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2/queryparam"
	"github.com/go-chi/chi"
	"github.com/goccy/go-yaml"
	"github.com/mitchellh/mapstructure"
	"github.com/stretchr/testify/assert"
)

type testContext struct {
	op *Operation
	r  *http.Request
	w  http.ResponseWriter
}

func (ctx *testContext) Operation() *Operation {
	return ctx.op
}

func (ctx *testContext) Matched() string {
	return chi.RouteContext(ctx.r.Context()).RoutePattern()
}

func (ctx *testContext) Context() context.Context {
	return ctx.r.Context()
}

func (ctx *testContext) Method() string {
	return ctx.r.Method
}

func (ctx *testContext) Host() string {
	return ctx.r.Host
}

func (ctx *testContext) URL() url.URL {
	return *ctx.r.URL
}

func (ctx *testContext) Param(name string) string {
	return chi.URLParam(ctx.r, name)
}

func (ctx *testContext) Query(name string) string {
	return queryparam.Get(ctx.r.URL.RawQuery, name)
}

func (ctx *testContext) Header(name string) string {
	return ctx.r.Header.Get(name)
}

func (ctx *testContext) EachHeader(cb func(name, value string)) {
	for name, values := range ctx.r.Header {
		for _, value := range values {
			cb(name, value)
		}
	}
}

func (ctx *testContext) Body() ([]byte, error) {
	return io.ReadAll(ctx.r.Body)
}

func (ctx *testContext) BodyReader() io.Reader {
	return ctx.r.Body
}

func (ctx *testContext) GetMultipartForm() (*multipart.Form, error) {
	err := ctx.r.ParseMultipartForm(8 * 1024)
	return ctx.r.MultipartForm, err
}

func (ctx *testContext) SetReadDeadline(deadline time.Time) error {
	return http.NewResponseController(ctx.w).SetReadDeadline(deadline)
}

func (ctx *testContext) SetStatus(code int) {
	ctx.w.WriteHeader(code)
}

func (ctx *testContext) AppendHeader(name string, value string) {
	ctx.w.Header().Add(name, value)
}

func (ctx *testContext) SetHeader(name string, value string) {
	ctx.w.Header().Set(name, value)
}

func (ctx *testContext) BodyWriter() io.Writer {
	return ctx.w
}

type testAdapter struct {
	router chi.Router
}

func (a *testAdapter) Handle(op *Operation, handler func(Context)) {
	a.router.MethodFunc(op.Method, op.Path, func(w http.ResponseWriter, r *http.Request) {
		handler(&testContext{op: op, r: r, w: w})
	})
}

func (a *testAdapter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	a.router.ServeHTTP(w, r)
}

func NewTestAdapter(r chi.Router, config Config) API {
	return NewAPI(config, &testAdapter{router: r})
}

func TestFeatures(t *testing.T) {
	for _, feature := range []struct {
		Name     string
		Register func(t *testing.T, api API)
		Method   string
		URL      string
		Headers  map[string]string
		Assert   func(t *testing.T, resp *httptest.ResponseRecorder)
	}{
		{
			Name: "params",
			Register: func(t *testing.T, api API) {
				Register(api, Operation{
					Method: http.MethodGet,
					Path:   "/test-params/{string}/{int}",
				}, func(ctx context.Context, input *struct {
					PathString   string    `path:"string"`
					PathInt      int       `path:"int"`
					QueryString  string    `query:"string"`
					QueryInt     int       `query:"int"`
					QueryDefault float32   `query:"def" default:"135" example:"5"`
					QueryBefore  time.Time `query:"before"`
					QueryDate    time.Time `query:"date" timeFormat:"2006-01-02"`
					HeaderString string    `header:"String"`
					HeaderInt    int       `header:"Int"`
				}) (*struct{}, error) {
					assert.Equal(t, "foo", input.PathString)
					assert.Equal(t, 123, input.PathInt)
					assert.Equal(t, "bar", input.QueryString)
					assert.Equal(t, 456, input.QueryInt)
					assert.Equal(t, float32(135), input.QueryDefault)
					assert.True(t, input.QueryBefore.Equal(time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC)))
					assert.True(t, input.QueryDate.Equal(time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)))
					assert.Equal(t, "baz", input.HeaderString)
					assert.Equal(t, 789, input.HeaderInt)
					return nil, nil
				})
			},
			Method:  http.MethodGet,
			URL:     "/test-params/foo/123?string=bar&int=456&before=2023-01-01T12:00:00Z&date=2023-01-01",
			Headers: map[string]string{"string": "baz", "int": "789"},
		},
		{
			Name: "response",
			Register: func(t *testing.T, api API) {
				type Resp struct {
					Foo  string `header:"foo"`
					Body struct {
						Greeting string `json:"greeting"`
					}
				}

				Register(api, Operation{
					Method: http.MethodGet,
					Path:   "/response",
				}, func(ctx context.Context, input *struct{}) (*Resp, error) {
					resp := &Resp{}
					resp.Foo = "foo"
					resp.Body.Greeting = "Hello, world!"
					return resp, nil
				})
			},
			Method: http.MethodGet,
			URL:    "/response",
			Assert: func(t *testing.T, resp *httptest.ResponseRecorder) {
				assert.Equal(t, http.StatusOK, resp.Code)
				assert.Equal(t, "foo", resp.Header().Get("Foo"))
				assert.JSONEq(t, `{"$schema": "https:///schemas/RespBody.json", "greeting":"Hello, world!"}`, resp.Body.String())
			},
		},
	} {
		t.Run(feature.Name, func(t *testing.T) {
			r := chi.NewRouter()
			api := NewTestAdapter(r, DefaultConfig("Features Test API", "1.0.0"))
			feature.Register(t, api)

			req, _ := http.NewRequest(feature.Method, feature.URL, nil)
			for k, v := range feature.Headers {
				req.Header.Set(k, v)
			}
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			b, _ := yaml.Marshal(api.OpenAPI())
			t.Log(string(b))
			assert.Less(t, w.Code, 300, w.Body.String())
			if feature.Assert != nil {
				feature.Assert(t, w)
			}
		})
	}
}

type ExhaustiveErrorsInputBody struct {
	Name  string `json:"name" maxLength:"10"`
	Count int    `json:"count" minimum:"1"`
}

func (b *ExhaustiveErrorsInputBody) Resolve(ctx Context) []error {
	return []error{fmt.Errorf("body resolver error")}
}

type ExhaustiveErrorsInput struct {
	ID   string                    `path:"id" maxLength:"5"`
	Body ExhaustiveErrorsInputBody `json:"body"`
}

func (i *ExhaustiveErrorsInput) Resolve(ctx Context) []error {
	return []error{&ErrorDetail{
		Location: "path.id",
		Message:  "input resolver error",
		Value:    i.ID,
	}}
}

type ExhaustiveErrorsOutput struct {
}

func TestExhaustiveErrors(t *testing.T) {
	r := chi.NewRouter()
	app := NewTestAdapter(r, DefaultConfig("Test API", "1.0.0"))
	Register(app, Operation{
		OperationID: "test",
		Method:      http.MethodPut,
		Path:        "/errors/{id}",
	}, func(ctx context.Context, input *ExhaustiveErrorsInput) (*ExhaustiveErrorsOutput, error) {
		return &ExhaustiveErrorsOutput{}, nil
	})

	req, _ := http.NewRequest(http.MethodPut, "/errors/123456", strings.NewReader(`{"name": "12345678901", "count": 0}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnprocessableEntity, w.Code)
	assert.JSONEq(t, `{
		"$schema": "https:///schemas/ErrorModel.json",
		"title": "Unprocessable Entity",
		"status": 422,
		"detail": "validation failed",
		"errors": [
			{
				"message": "expected length <= 5",
				"location": "path.id",
				"value": "123456"
			}, {
				"message": "expected length <= 10",
				"location": "body.name",
				"value": "12345678901"
			}, {
				"message": "expected number >= 1",
				"location": "body.count",
				"value": 0
			}, {
				"message": "input resolver error",
				"location": "path.id",
				"value": "123456"
			}, {
				"message": "body resolver error"
			}
		]
	}`, w.Body.String())
}

type NestedResolversStruct struct {
	Field2 string `json:"field2"`
}

func (b *NestedResolversStruct) Resolve(ctx Context, prefix *PathBuffer) []error {
	return []error{&ErrorDetail{
		Location: prefix.With("field2"),
		Message:  "resolver error",
		Value:    b.Field2,
	}}
}

var _ ResolverWithPath = (*NestedResolversStruct)(nil)

type NestedResolversBody struct {
	Field1 map[string][]NestedResolversStruct `json:"field1"`
}

type NestedResolverRequest struct {
	Body NestedResolversBody
}

func TestNestedResolverWithPath(t *testing.T) {
	r := chi.NewRouter()
	app := NewTestAdapter(r, DefaultConfig("Test API", "1.0.0"))
	Register(app, Operation{
		OperationID: "test",
		Method:      http.MethodPut,
		Path:        "/test",
	}, func(ctx context.Context, input *NestedResolverRequest) (*struct{}, error) {
		return nil, nil
	})

	req, _ := http.NewRequest(http.MethodPut, "/test", strings.NewReader(`{"field1": {"foo": [{"field2": "bar"}]}}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnprocessableEntity, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), `"location":"body.field1.foo[0].field2"`)
}

func BenchmarkSecondDecode(b *testing.B) {
	type MediumSized struct {
		ID   int      `json:"id"`
		Name string   `json:"name"`
		Tags []string `json:"tags"`
		// Created time.Time `json:"created"`
		// Updated time.Time `json:"updated"`
		Rating float64 `json:"rating"`
		Owner  struct {
			ID    int    `json:"id"`
			Name  string `json:"name"`
			Email string `json:"email"`
		}
		Categories []struct {
			Name    string   `json:"name"`
			Order   int      `json:"order"`
			Visible bool     `json:"visible"`
			Aliases []string `json:"aliases"`
		}
	}

	data := []byte(`{
		"id": 123,
		"name": "Test",
		"tags": ["one", "two", "three"],
		"created": "2021-01-01T12:00:00Z",
		"updated": "2021-01-01T12:00:00Z",
		"rating": 5.0,
		"owner": {
			"id": 4,
			"name": "Alice",
			"email": "alice@example.com"
		},
		"categories": [
			{
				"name": "First",
				"order": 1,
				"visible": true
			},
			{
				"name": "Second",
				"order": 2,
				"visible": false,
				"aliases": ["foo", "bar"]
			}
		]
	}`)

	pb := NewPathBuffer([]byte{}, 0)
	res := &ValidateResult{}
	registry := NewMapRegistry("#/components/schemas/", DefaultSchemaNamer)
	fmt.Println("name", reflect.TypeOf(MediumSized{}).Name())
	schema := registry.Schema(reflect.TypeOf(MediumSized{}), false, "")

	b.Run("json.Unmarshal", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			var tmp any
			if err := json.Unmarshal(data, &tmp); err != nil {
				panic(err)
			}

			Validate(registry, schema, pb, ModeReadFromServer, tmp, res)

			var out MediumSized
			if err := json.Unmarshal(data, &out); err != nil {
				panic(err)
			}
		}
	})

	b.Run("mapstructure.Decode", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			var tmp any
			if err := json.Unmarshal(data, &tmp); err != nil {
				panic(err)
			}

			Validate(registry, schema, pb, ModeReadFromServer, tmp, res)

			var out MediumSized
			if err := mapstructure.Decode(tmp, &out); err != nil {
				panic(err)
			}
		}
	})
}

// var jsonData = []byte(`[
//   {
//     "desired_state": "ON",
//     "etag": "203f7a94",
//     "id": "bvt3",
//     "name": "BVT channel - CNN Plus 2",
//     "org": "t2dev",
//     "self": "https://api.istreamplanet.com/v2/t2dev/channels/bvt3",
// 		"created": "2021-01-01T12:00:00Z",
// 		"count": 18273,
// 		"rating": 5.0,
// 		"tags": ["one", "three"],
//     "source": {
//       "id": "stn-dd4j42ytxmajz6xz",
//       "self": "https://api.istreamplanet.com/v2/t2dev/sources/stn-dd4j42ytxmajz6xz"
//     }
//   },
//   {
//     "desired_state": "ON",
//     "etag": "WgY5zNTPn3ECf_TSPAgL9Y-E9doUaRxAdjukGsCt_sQ",
//     "id": "bvt2",
//     "name": "BVT channel - Hulu",
//     "org": "t2dev",
//     "self": "https://api.istreamplanet.com/v2/t2dev/channels/bvt2",
// 		"created": "2023-01-01T12:01:00Z",
// 		"count": 1,
// 		"rating": 4.5,
// 		"tags": ["two"],
//     "source": {
//       "id": "stn-yuqvm3hzowrv6rph",
//       "self": "https://api.istreamplanet.com/v2/t2dev/sources/stn-yuqvm3hzowrv6rph"
//     }
//   },
//   {
//     "desired_state": "ON",
//     "etag": "1GaleyULVhpmHJXCJPUGSeBM2YYAZGBYKVcR5sZu5U8",
//     "id": "bvt1",
//     "name": "BVT channel - Hulu",
//     "org": "t2dev",
//     "self": "https://api.istreamplanet.com/v2/t2dev/channels/bvt1",
// 		"created": "2023-01-01T12:00:00Z",
// 		"count": 57,
// 		"rating": 3.5,
// 		"tags": ["one", "two"],
//     "source": {
//       "id": "stn-fc6sqodptbz5keuy",
//       "self": "https://api.istreamplanet.com/v2/t2dev/sources/stn-fc6sqodptbz5keuy"
//     }
//   }
// ]`)

// type Summary struct {
// 	DesiredState string    `json:"desired_state"`
// 	ETag         string    `json:"etag"`
// 	ID           string    `json:"id"`
// 	Name         string    `json:"name"`
// 	Org          string    `json:"org"`
// 	Self         string    `json:"self"`
// 	Created      time.Time `json:"created"`
// 	Count        int       `json:"count"`
// 	Rating       float64   `json:"rating"`
// 	Tags         []string  `json:"tags"`
// 	Source       struct {
// 		ID   string `json:"id"`
// 		Self string `json:"self"`
// 	} `json:"source"`
// }

// func BenchmarkMarshalStructJSON(b *testing.B) {
// 	var summaries []Summary
// 	if err := stdjson.Unmarshal(jsonData, &summaries); err != nil {
// 		panic(err)
// 	}

// 	b.ResetTimer()
// 	b.ReportAllocs()
// 	for i := 0; i < b.N; i++ {
// 		b, _ := stdjson.Marshal(summaries)
// 		_ = b
// 	}
// }

// func BenchmarkMarshalAnyJSON(b *testing.B) {
// 	var summaries any
// 	stdjson.Unmarshal(jsonData, &summaries)

// 	b.ResetTimer()
// 	b.ReportAllocs()
// 	for i := 0; i < b.N; i++ {
// 		b, _ := stdjson.Marshal(summaries)
// 		_ = b
// 	}
// }

// func BenchmarkUnmarshalStructJSON(b *testing.B) {
// 	var summaries []Summary

// 	b.ResetTimer()
// 	b.ReportAllocs()
// 	for i := 0; i < b.N; i++ {
// 		summaries = nil
// 		stdjson.Unmarshal(jsonData, &summaries)
// 		_ = summaries
// 	}
// }

// func BenchmarkUnmarshalAnyJSON(b *testing.B) {
// 	var summaries any

// 	b.ResetTimer()
// 	b.ReportAllocs()
// 	for i := 0; i < b.N; i++ {
// 		summaries = nil
// 		stdjson.Unmarshal(jsonData, &summaries)
// 		_ = summaries
// 	}
// }

// func BenchmarkMarshalStructJSONiter(b *testing.B) {
// 	var summaries []Summary
// 	json.Unmarshal(jsonData, &summaries)

// 	b.ResetTimer()
// 	b.ReportAllocs()
// 	for i := 0; i < b.N; i++ {
// 		b, _ := json.Marshal(summaries)
// 		_ = b
// 	}
// }

// func BenchmarkMarshalAnyJSONiter(b *testing.B) {
// 	var summaries any
// 	json.Unmarshal(jsonData, &summaries)

// 	b.ResetTimer()
// 	b.ReportAllocs()
// 	for i := 0; i < b.N; i++ {
// 		b, _ := json.Marshal(summaries)
// 		_ = b
// 	}
// }

// func BenchmarkUnmarshalStructJSONiter(b *testing.B) {
// 	var summaries []Summary

// 	b.ResetTimer()
// 	b.ReportAllocs()
// 	for i := 0; i < b.N; i++ {
// 		summaries = nil
// 		json.Unmarshal(jsonData, &summaries)
// 		_ = summaries
// 	}
// }

// func BenchmarkUnmarshalAnyJSONiter(b *testing.B) {
// 	var summaries any

// 	b.ResetTimer()
// 	b.ReportAllocs()
// 	for i := 0; i < b.N; i++ {
// 		summaries = nil
// 		json.Unmarshal(jsonData, &summaries)
// 		_ = summaries
// 	}
// }
