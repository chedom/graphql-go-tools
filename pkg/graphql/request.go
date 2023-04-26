package graphql

import (
	"encoding/json"
	"errors"
	"io"
	"io/ioutil"
	"net/http"
	"strings"

	lru "github.com/hashicorp/golang-lru"

	"github.com/jensneuse/graphql-go-tools/pkg/ast"
	"github.com/jensneuse/graphql-go-tools/pkg/astparser"
	"github.com/jensneuse/graphql-go-tools/pkg/engine/resolve"
	"github.com/jensneuse/graphql-go-tools/pkg/middleware/operation_complexity"
	"github.com/jensneuse/graphql-go-tools/pkg/operationreport"
	"github.com/jensneuse/graphql-go-tools/pkg/pool"
)

const (
	defaultInrospectionQueryName = "IntrospectionQuery"
	schemaFieldName              = "__schema"
)

type OperationType ast.OperationType

const (
	OperationTypeUnknown      OperationType = OperationType(ast.OperationTypeUnknown)
	OperationTypeQuery        OperationType = OperationType(ast.OperationTypeQuery)
	OperationTypeMutation     OperationType = OperationType(ast.OperationTypeMutation)
	OperationTypeSubscription OperationType = OperationType(ast.OperationTypeSubscription)
)

var (
	ErrEmptyRequest = errors.New("the provided request is empty")
	ErrNilSchema    = errors.New("the provided schema is nil")
)

type Request struct {
	OperationName string          `json:"operationName"`
	Variables     json.RawMessage `json:"variables"`
	Query         string          `json:"query"`

	Map   json.RawMessage
	Files map[string]resolve.FileUpload

	document     ast.Document
	isParsed     bool
	isNormalized bool
	request      resolve.Request

	validForSchema map[uint64]ValidationResult

	DocumentCache *lru.Cache
}

func UnmarshalRequest(reader io.Reader, request *Request) error {
	requestBytes, err := ioutil.ReadAll(reader)
	if err != nil {
		return err
	}

	if len(requestBytes) == 0 {
		return ErrEmptyRequest
	}

	return json.Unmarshal(requestBytes, &request)
}

func UnmarshalMultiPartRequest(r *http.Request, request *Request, maxMemory int64) error {
	err := r.ParseMultipartForm(maxMemory)
	if err != nil {
		return err
	}

	err = json.Unmarshal([]byte(r.Form.Get("operations")), &request)
	if err != nil {
		return err
	}

	uploadMaps := map[string][]string{}

	request.Map = []byte(r.Form.Get("map"))

	err = json.Unmarshal(request.Map, &uploadMaps)
	if err != nil {
		return err
	}

	if request.Files == nil {
		request.Files = map[string]resolve.FileUpload{}
	}

	for key, _ := range uploadMaps {
		if file, header, err := r.FormFile(key); err != nil {
			return err
		} else {
			request.Files[key] = resolve.FileUpload{
				File:     file,
				Size:     header.Size,
				Filename: header.Filename,
			}
		}
	}

	return nil
}

func UnmarshalHttpRequest(r *http.Request, request *Request, maxMemory int64) error {
	request.request.Header = r.Header

	var result error = nil

	if r.Method == "POST" {
		contentType := strings.SplitN(r.Header.Get("Content-Type"), ";", 2)[0]

		switch contentType {
		case "text/plain", "application/json":
			result = UnmarshalRequest(r.Body, request)
		case "multipart/form-data":
			result = UnmarshalMultiPartRequest(r, request, maxMemory)
		}
	} else {
		result = UnmarshalRequest(r.Body, request)
	}

	return result
}

func (r *Request) SetHeader(header http.Header) {
	r.request.Header = header
}

func (r *Request) CalculateComplexity(complexityCalculator ComplexityCalculator, schema *Schema) (ComplexityResult, error) {
	if schema == nil {
		return ComplexityResult{}, ErrNilSchema
	}

	report := r.parseQueryOnce()
	if report.HasErrors() {
		return complexityResult(
			operation_complexity.OperationStats{},
			[]operation_complexity.RootFieldStats{},
			report,
		)
	}

	return complexityCalculator.Calculate(&r.document, &schema.document)
}

func (r *Request) Print(writer io.Writer) (n int, err error) {
	report := r.parseQueryOnce()
	if report.HasErrors() {
		return 0, report
	}

	return writer.Write(r.document.Input.RawBytes)
}

func (r *Request) IsNormalized() bool {
	return r.isNormalized
}

func (r *Request) parseQueryOnce() (report operationreport.Report) {
	if r.isParsed {
		return report
	}

	hash := pool.Hash64.Get()
	hash.Reset()
	defer pool.Hash64.Put(hash)
	_, _ = hash.Write([]byte(r.Query))
	cacheKey := hash.Sum64()

	r.isParsed = true

	if r.DocumentCache != nil {
		if cached, ok := r.DocumentCache.Get(cacheKey); ok {
			if doc, ok := cached.(*ast.Document); ok {
				cloned := doc.Clone()
				r.document = *cloned
				return report
			}
		}
	}

	r.document, report = astparser.ParseGraphqlDocumentString(r.Query)

	if r.DocumentCache != nil && !report.HasErrors() {
		cloned := r.document.Clone()
		r.DocumentCache.Add(cacheKey, cloned)
	}

	return report
}

func (r *Request) IsIntrospectionQuery() (result bool, err error) {
	report := r.parseQueryOnce()
	if report.HasErrors() {
		return false, report
	}

	if r.OperationName == defaultInrospectionQueryName {
		return true, nil
	}

	if len(r.document.RootNodes) == 0 {
		return
	}

	rootNode := r.document.RootNodes[0]
	if rootNode.Kind != ast.NodeKindOperationDefinition {
		return
	}

	operationDef := r.document.OperationDefinitions[rootNode.Ref]
	if operationDef.OperationType != ast.OperationTypeQuery {
		return
	}
	if !operationDef.HasSelections {
		return
	}

	selectionSet := r.document.SelectionSets[operationDef.SelectionSet]
	if len(selectionSet.SelectionRefs) == 0 {
		return
	}

	selection := r.document.Selections[selectionSet.SelectionRefs[0]]
	if selection.Kind != ast.SelectionKindField {
		return
	}

	return r.document.FieldNameUnsafeString(selection.Ref) == schemaFieldName, nil
}

func (r *Request) OperationType() (OperationType, error) {
	report := r.parseQueryOnce()
	if report.HasErrors() {
		return OperationTypeUnknown, report
	}

	for _, rootNode := range r.document.RootNodes {
		if rootNode.Kind != ast.NodeKindOperationDefinition {
			continue
		}

		if r.OperationName != "" && r.document.OperationDefinitionNameString(rootNode.Ref) != r.OperationName {
			continue
		}

		opType := r.document.OperationDefinitions[rootNode.Ref].OperationType
		return OperationType(opType), nil
	}

	return OperationTypeUnknown, nil
}

func (r *Request) OperationDocument() (*ast.Document, error) {
	report := r.parseQueryOnce()
	if report.HasErrors() {
		return nil, report
	}

	return &r.document, nil
}
