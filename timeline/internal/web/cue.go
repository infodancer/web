package web

import (
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	cuejson "cuelang.org/go/encoding/json"
)

//go:embed schemas/*.cue
var schemasFS embed.FS

// cueSchemas holds compiled CUE schemas for validation.
type cueSchemas struct {
	ctx   *cue.Context
	value cue.Value
}

func loadCueSchemas() (*cueSchemas, error) {
	ctx := cuecontext.New()

	data, err := schemasFS.ReadFile("schemas/timeline.cue")
	if err != nil {
		return nil, fmt.Errorf("read schema: %w", err)
	}

	v := ctx.CompileBytes(data)
	if v.Err() != nil {
		return nil, fmt.Errorf("compile schema: %w", v.Err())
	}

	return &cueSchemas{ctx: ctx, value: v}, nil
}

// validate checks data against a named schema definition (e.g. "#Event").
func (s *cueSchemas) validate(schemaName string, data []byte) error {
	schema := s.value.LookupPath(cue.ParsePath(schemaName))
	if !schema.Exists() {
		return fmt.Errorf("schema %q not found", schemaName)
	}

	jsonExpr, err := cuejson.Extract("input", data)
	if err != nil {
		return fmt.Errorf("parse input: %w", err)
	}

	inputVal := s.ctx.BuildExpr(jsonExpr)
	if inputVal.Err() != nil {
		return fmt.Errorf("build input: %w", inputVal.Err())
	}

	unified := schema.Unify(inputVal)
	if err := unified.Validate(); err != nil {
		return err
	}
	return nil
}

// ---- Content negotiation ----

const (
	contentTypeJSON = "application/json"
	contentTypeCUE  = "application/cue"
)

// requestContentType returns the content type of the request body.
func requestContentType(r *http.Request) string {
	ct := r.Header.Get("Content-Type")
	if strings.Contains(ct, "cue") {
		return contentTypeCUE
	}
	return contentTypeJSON
}

// responseContentType determines the desired response format from Accept header.
func responseContentType(r *http.Request) string {
	accept := r.Header.Get("Accept")
	if strings.Contains(accept, "cue") {
		return contentTypeCUE
	}
	return contentTypeJSON
}

// decodeRequest reads the request body as JSON, optionally parsing from CUE first.
// If the Content-Type is CUE, it parses CUE into JSON, then validates against
// the named schema, then unmarshals into dest.
func (s *Server) decodeRequest(r *http.Request, schemaName string, dest any) error {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return fmt.Errorf("read body: %w", err)
	}

	ct := requestContentType(r)
	var jsonBytes []byte

	if ct == contentTypeCUE {
		// Parse CUE to JSON
		cctx := cuecontext.New()
		if s.cue != nil {
			cctx = s.cue.ctx
		}
		v := cctx.CompileBytes(body)
		if v.Err() != nil {
			return fmt.Errorf("invalid CUE: %w", v.Err())
		}
		jsonBytes, err = v.MarshalJSON()
		if err != nil {
			return fmt.Errorf("CUE to JSON: %w", err)
		}
	} else {
		jsonBytes = body
	}

	// Validate against schema if provided
	if schemaName != "" && s.cue != nil {
		if err := s.cue.validate(schemaName, jsonBytes); err != nil {
			return fmt.Errorf("validation: %w", err)
		}
	}

	return json.Unmarshal(jsonBytes, dest)
}

// apiRespond writes the response in the negotiated format.
func apiRespond(w http.ResponseWriter, r *http.Request, status int, data any) {
	if responseContentType(r) == contentTypeCUE {
		ctx := cuecontext.New()
		jsonBytes, err := json.Marshal(data)
		if err != nil {
			apiError(w, http.StatusInternalServerError, err.Error())
			return
		}
		v := ctx.CompileBytes(jsonBytes)
		cueBytes := []byte(fmt.Sprintf("%v", v))
		w.Header().Set("Content-Type", contentTypeCUE)
		w.WriteHeader(status)
		w.Write(cueBytes)
		return
	}
	apiJSON(w, status, data)
}
