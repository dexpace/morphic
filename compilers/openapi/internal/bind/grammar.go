// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package bind

import "strings"

// state is where the OpenAPI grammar stands after reading a pointer's tokens.
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
	sExampleMap
	sExample
	sLinkMap
	sLink
	sSecuritySchemeMap
	sSecurityScheme
	sObject
	sRawEntries
)

// fields maps the fields an object state defines to the state each leads to,
// as the parser's model defines them (TestGrammar_FieldsAreTheModels). A field
// an object leaves out is raw YAML to the model, as is every x- key.
var fields = map[state]map[string]state{
	sDocument: {
		"openapi": sValue, "info": sObject, "jsonSchemaDialect": sValue, "servers": sObject, "paths": sPaths,
		"webhooks": sPathItemMap, "components": sComponents, "security": sObject, "tags": sObject,
		"externalDocs": sObject, "$self": sValue,
	},
	sComponents: {
		"schemas": sSchemaMap, "responses": sResponseMap, "parameters": sParameterMap, "examples": sExampleMap,
		"requestBodies": sRequestBodyMap, "headers": sHeaderMap, "securitySchemes": sSecuritySchemeMap,
		"links": sLinkMap, "callbacks": sCallbackMap, "pathItems": sPathItemMap,
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
		"schema": sSchema, "example": sRaw, "examples": sExampleMap, "content": sMediaTypeMap,
	},
	sHeader: {
		"description": sValue, "required": sValue, "deprecated": sValue, "style": sValue, "explode": sValue,
		"schema": sSchema, "example": sRaw, "examples": sExampleMap, "content": sMediaTypeMap,
	},
	sRequestBody: {"description": sValue, "content": sMediaTypeMap, "required": sValue},
	sResponse:    {"description": sValue, "headers": sHeaderMap, "content": sMediaTypeMap, "links": sLinkMap},
	sMediaType: {
		"schema": sSchema, "itemSchema": sSchema, "example": sRaw, "examples": sExampleMap,
		"encoding": sEncodingMap, "prefixEncoding": sEncodingList, "itemEncoding": sEncoding,
	},
	sEncoding: {
		"contentType": sValue, "headers": sHeaderMap, "style": sValue, "explode": sValue, "allowReserved": sValue,
	},
	sExample: {
		"summary": sValue, "description": sValue, "value": sRaw, "dataValue": sRaw, "serializedValue": sValue,
		"externalValue": sValue,
	},
	sLink: {
		"operationRef": sValue, "operationId": sValue, "parameters": sRawEntries, "requestBody": sRaw,
		"description": sValue, "server": sObject,
	},
	sSecurityScheme: {
		"type": sValue, "description": sValue, "name": sValue, "in": sValue, "scheme": sValue,
		"bearerFormat": sValue, "flows": sObject, "openIdConnectUrl": sValue, "oauth2MetadataUrl": sValue,
		"deprecated": sValue,
	},
}

// schemaFields is what a Schema Object holds under each keyword the model
// reads: a schema, a map or list of them, a literal kept raw, a list of
// literals, or a value.
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
	for _, k := range []string{"example", "default", "const"} {
		m[k] = sRaw
	}
	for _, k := range []string{"enum", "examples"} {
		m[k] = sRawEntries
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

// unmodelled maps the fields an object state defines that the parser's model
// names no member for, and the load phase models itself, to the state each
// leads to: OpenAPI 3.2's components/mediaTypes (GitHub #856). They are kept
// apart from fields, which mirrors the model (TestGrammar_FieldsAreTheModels).
var unmodelled = map[state]map[string]state{sComponents: {"mediaTypes": sMediaTypeMap}}

// entries maps a container state to the state of each of its entries.
var entries = map[state]state{
	sSchemaMap:         sSchema,
	sSchemaList:        sSchema,
	sPaths:             sPathItem,
	sPathItemMap:       sPathItem,
	sOperationMap:      sOperation,
	sParameterList:     sParameter,
	sParameterMap:      sParameter,
	sHeaderMap:         sHeader,
	sRequestBodyMap:    sRequestBody,
	sResponses:         sResponse,
	sResponseMap:       sResponse,
	sMediaTypeMap:      sMediaType,
	sEncodingMap:       sEncoding,
	sEncodingList:      sEncoding,
	sCallbackMap:       sCallback,
	sCallback:          sPathItem,
	sExampleMap:        sExample,
	sLinkMap:           sLink,
	sSecuritySchemeMap: sSecurityScheme,
	sRawEntries:        sRaw,
}

// extensible holds the containers whose x- keys are extensions rather than
// entries: those the specification gives patterned fields of their own.
var extensible = map[state]bool{sPaths: true, sResponses: true, sCallback: true}

// referenceKinds holds the states of the objects modelled as a reference object
// when they write a $ref: the parser's own kinds, and a Media Type Object, which
// the load phase models (GitHub #856). The parser's resolver, not this package,
// resolves those, and a pointer read through one reads on in the object it
// names.
var referenceKinds = map[state]bool{
	sPathItem: true, sParameter: true, sHeader: true, sRequestBody: true, sResponse: true, sCallback: true,
	sExample: true, sLink: true, sSecurityScheme: true, sMediaType: true,
}

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
	if next, ok := unmodelled[s][token]; ok {
		return next
	}
	return sRaw
}

// stateOf is the state tokens reach from s.
func stateOf(s state, tokens []string) state {
	for _, t := range tokens {
		s = step(s, t)
	}
	return s
}

// roleOf is the role a position read at state s has.
func roleOf(s state) Role {
	switch s {
	case sSchema:
		return Schema
	case sRaw:
		return Raw
	default:
		return Object
	}
}

// modelled reports whether the model holds a position read at s as an object
// or a list of its own, rather than as raw YAML or a scalar value.
func modelled(s state) bool {
	return s != sRaw && s != sValue
}
