package bind

import "strings"

// Role is what a position of the document holds, as a schema reference reads
// it.
type Role uint8

const (
	// NoRole is a position the document does not hold.
	NoRole Role = iota
	// Schema is a Schema Object the OpenAPI grammar places there.
	Schema
	// Raw is YAML the model keeps as written: an extension, a key no object
	// defines, or a literal such as an example or an enum member. A reference
	// reads it as a schema.
	Raw
	// Object is any other object or value of the grammar, which no schema
	// reference may name.
	Object
)

// String spells r for a table.
func (r Role) String() string {
	switch r {
	case Schema:
		return "schema"
	case Raw:
		return "raw"
	case Object:
		return "object"
	default:
		return "none"
	}
}

// state is where the grammar stands after reading a pointer's tokens.
type state uint8

const (
	sRaw state = iota
	sValue
	sDocument
	sComponents
	sSchemaMap
	sSchema
	sSchemaList
	sPaths
	sPathItemMap
	sPathItem
	sOperationMap
	sOperation
	sParameterList
	sParameterMap
	sParameter
	sHeaderMap
	sHeader
	sRequestBodyMap
	sRequestBody
	sResponses
	sResponseMap
	sResponse
	sMediaTypeMap
	sMediaType
	sEncodingMap
	sEncodingList
	sEncoding
	sCallbackMap
	sCallback
	sObject
)

// fields maps the fields an object state defines to the state each leads to.
// A field an object leaves out is raw YAML to the model, as is every x- key.
var fields = map[state]map[string]state{
	sDocument: {
		"openapi": sValue, "info": sObject, "jsonSchemaDialect": sValue, "servers": sObject, "paths": sPaths,
		"webhooks": sPathItemMap, "components": sComponents, "security": sObject, "tags": sObject,
		"externalDocs": sObject, "$self": sValue,
	},
	sComponents: {
		"schemas": sSchemaMap, "responses": sResponseMap, "parameters": sParameterMap, "examples": sObject,
		"requestBodies": sRequestBodyMap, "headers": sHeaderMap, "securitySchemes": sObject, "links": sObject,
		"callbacks": sCallbackMap, "pathItems": sPathItemMap, "mediaTypes": sMediaTypeMap,
	},
	sSchema: schemaFields(),
	sPathItem: {
		"$ref": sValue, "summary": sValue, "description": sValue, "servers": sObject, "parameters": sParameterList,
		"get": sOperation, "put": sOperation, "post": sOperation, "delete": sOperation, "options": sOperation,
		"head": sOperation, "patch": sOperation, "trace": sOperation, "query": sOperation,
		"additionalOperations": sOperationMap,
	},
	sOperation: {
		"tags": sObject, "summary": sValue, "description": sValue, "externalDocs": sObject, "operationId": sValue,
		"parameters": sParameterList, "requestBody": sRequestBody, "responses": sResponses,
		"callbacks": sCallbackMap, "deprecated": sValue, "security": sObject, "servers": sObject,
	},
	sParameter: {
		"name": sValue, "in": sValue, "description": sValue, "required": sValue, "deprecated": sValue,
		"allowEmptyValue": sValue, "style": sValue, "explode": sValue, "allowReserved": sValue,
		"schema": sSchema, "example": sRaw, "examples": sObject, "content": sMediaTypeMap,
	},
	sHeader: {
		"description": sValue, "required": sValue, "deprecated": sValue, "style": sValue, "explode": sValue,
		"schema": sSchema, "example": sRaw, "examples": sObject, "content": sMediaTypeMap,
	},
	sRequestBody: {"description": sValue, "content": sMediaTypeMap, "required": sValue},
	sResponse: {
		"summary": sValue, "description": sValue, "headers": sHeaderMap, "content": sMediaTypeMap, "links": sObject,
	},
	sMediaType: {
		"schema": sSchema, "itemSchema": sSchema, "example": sRaw, "examples": sObject, "encoding": sEncodingMap,
		"prefixEncoding": sEncodingList, "itemEncoding": sEncoding, "description": sValue,
	},
	sEncoding: {
		"contentType": sValue, "headers": sHeaderMap, "style": sValue, "explode": sValue, "allowReserved": sValue,
		"encoding": sEncodingMap, "prefixEncoding": sEncodingList, "itemEncoding": sEncoding,
	},
}

// schemaFields is what a Schema Object holds under each keyword the model
// reads: a schema, a map or list of them, a literal kept raw, or a value.
func schemaFields() map[string]state {
	m := map[string]state{}
	for _, k := range []string{"not", "if", "then", "else", "additionalProperties", "items", "contains",
		"propertyNames", "unevaluatedItems", "unevaluatedProperties", "contentSchema"} {
		m[k] = sSchema
	}
	for _, k := range []string{"properties", "patternProperties", "dependentSchemas", "$defs"} {
		m[k] = sSchemaMap
	}
	for _, k := range []string{"allOf", "anyOf", "oneOf", "prefixItems"} {
		m[k] = sSchemaList
	}
	for _, k := range []string{"example", "default", "const", "enum", "examples"} {
		m[k] = sRaw
	}
	for _, k := range []string{"$ref", "$id", "$schema", "$anchor", "type", "title", "description", "format",
		"multipleOf", "maximum", "minimum", "exclusiveMaximum", "exclusiveMinimum", "maxLength", "minLength",
		"pattern", "maxItems", "minItems", "uniqueItems", "maxProperties", "minProperties", "required",
		"minContains", "maxContains", "contentEncoding", "contentMediaType", "nullable", "readOnly", "writeOnly",
		"deprecated", "discriminator", "xml", "externalDocs"} {
		m[k] = sValue
	}
	return m
}

// entries maps a container state to the state of each of its entries.
var entries = map[state]state{
	sSchemaMap:      sSchema,
	sSchemaList:     sSchema,
	sPaths:          sPathItem,
	sPathItemMap:    sPathItem,
	sOperationMap:   sOperation,
	sParameterList:  sParameter,
	sParameterMap:   sParameter,
	sHeaderMap:      sHeader,
	sRequestBodyMap: sRequestBody,
	sResponses:      sResponse,
	sResponseMap:    sResponse,
	sMediaTypeMap:   sMediaType,
	sEncodingMap:    sEncoding,
	sEncodingList:   sEncoding,
	sCallbackMap:    sCallback,
	sCallback:       sPathItem,
}

// extensible holds the containers whose x- keys are extensions rather than
// entries: those the specification gives patterned fields of their own.
var extensible = map[state]bool{sPaths: true, sResponses: true, sCallback: true}

// step is the state one token below s.
func step(s state, token string) state {
	switch s {
	case sRaw:
		return sRaw
	case sValue, sObject:
		return sValue
	}
	if e, ok := entries[s]; ok {
		if strings.HasPrefix(token, "x-") && extensible[s] {
			return sRaw
		}
		return e
	}
	if next, ok := fields[s][token]; ok {
		return next
	}
	return sRaw
}

// roleOf is the role of the position tokens names from the document's root.
func roleOf(tokens []string) Role {
	s := sDocument
	for _, t := range tokens {
		s = step(s, t)
	}
	return roleOfState(s)
}

// roleOfState is the role a grammar state gives its position.
func roleOfState(s state) Role {
	switch s {
	case sSchema:
		return Schema
	case sRaw:
		return Raw
	default:
		return Object
	}
}
