package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"sort"
	"strings"

	"github.com/team-telnyx/telnyx-mock/generator/datareplacer"
	"github.com/team-telnyx/telnyx-mock/spec"
)

// GenerateParams is a parameters structure that's used to invoke Generate and
// its associated methods.
//
// This structure exists to avoid runaway paramete inflation for the Generate
// function, so that we can document individual parameters in a more organized
// way, and because it can conveniently encapsulate some unexported fields that
// Generate uses to track its progress.
type GenerateParams struct {
	// Expansions are the requested expansions for the current level of generation.
	//
	// nil if no expansions were requested, or we've recursed to a level where
	// none of the original expansions applied.
	Expansions *ExpansionLevel

	// PathParams, if set, is a collection that contains values for parameters
	// that were extracted from a request path. This is useful so that we can
	// reflect those values into responses for a more realistic effect.
	//
	// nil if there were no values extracted from the path.
	//
	// The value of this field is considered in a post-processing step for the
	// generator. It's not used in the generator at all.
	PathParams *PathParamsMap

	// RequestData is a collection of decoded data that was included as part of
	// the request's payload.
	//
	// It's used to find opportunities to reflect information included with a
	// request into the response to make responses look more accurate than
	// they'd otherwise be if they'd been generated from fixtures alone..
	RequestData map[string]interface{}

	// RequestMethod is the HTTP method of the URL being requested which we're
	// generating data for. It's used to decide between returning a deleted and
	// non-deleted schema in some cases.
	//
	// The value of this field is expected to stay stable across all levels of
	// recursion.
	RequestMethod string

	// RequestPath is the path of the URL being requested which we're
	// generating data for. It's used to populate the url property of any
	// nested lists that we generate.
	//
	// The value of this field is expected to stay stable across all levels of
	// recursion.
	RequestPath string

	// Specify whether the object should be wrapped in a list before being
	// returned.
	WrapWithList bool

	//
	// Private fields
	//

	// Schema representing the object that we're trying to generate.
	//
	// The value of this field will change as Generate recurses to the target
	// schema at that level of recursion.
	//
	// This field is required.
	schema *spec.Schema

	// context is a breadcrumb trail that's added to as Generate recurses. It's
	// not important for the final result, but is very useful for debugging.
	context string

	// example is a valid data sample for the target schema at this level of
	// recursion.
	//
	// nil means that were was no sample available. A valueWrapper instance
	// with an embedded nil means that there is a sample, and it's nil/null.
	example *valueWrapper
}

// DataGenerator generates fixture response data based off a response schema, a
// set of definitions, and a fixture store.
type DataGenerator struct {
	definitions map[string]*spec.Schema
	fixtures    *spec.Fixtures
}

// Generate generates a fixture response.
func (g *DataGenerator) Generate(dataSchema *spec.Schema, metaSchema *spec.Schema, params *GenerateParams) (interface{}, error) {
	// This just makes our context message readable in case there was no
	// request path specified.
	requestPathDisplay := params.RequestPath
	if requestPathDisplay == "" {
		requestPathDisplay = "(empty request path)"
	}

	data, err := g.generateInternal(&GenerateParams{
		Expansions:    params.Expansions,
		PathParams:    nil,
		RequestMethod: params.RequestMethod,
		RequestPath:   params.RequestPath,
		WrapWithList:  params.WrapWithList,

		schema: dataSchema.FlattenAllOf(),
		context: fmt.Sprintf("Responding to %s %s:\n",
			params.RequestMethod, requestPathDisplay),
		example: g.prepareSchemaExample(dataSchema),
	})

	if err != nil {
		return nil, fmt.Errorf("error generating data response: %v", err)
	}

	var meta interface{}

	if metaSchema != nil {
		meta, err = g.generateInternal(&GenerateParams{
			Expansions:    nil,
			PathParams:    nil,
			RequestMethod: params.RequestMethod,
			RequestPath:   params.RequestPath,
			WrapWithList:  false,

			schema: metaSchema.FlattenAllOf(),
			context: fmt.Sprintf("Responding to %s %s:\n",
				params.RequestMethod, requestPathDisplay),
			example: g.prepareSchemaExample(metaSchema),
		})

		if err != nil {
			return nil, fmt.Errorf("error generating meta response: %v", err)
		}
	}

	if params.PathParams != nil {
		// Passses through the generated data and replaces IDs that existed in
		// the fixtures with IDs that were extracted from the request path, if
		// and where appropriate.
		//
		// Note that the path params are mutated by the function, but we return
		// them anyway to make the control flow here more clear.
		pathParams := recordAndReplaceIDs(params.PathParams, data)

		// Passes through the generated data again to replace the values of any old
		// IDs that we replaced. This is a separate step because IDs could have
		// been found and replace at any point in the generation process.
		distributeReplacedIDs(pathParams, data)
	}

	// In `POST` requests we reflect input parameters into responses to try and
	// simulate a more realistic create or update operation.
	if params.RequestMethod == http.MethodPost || params.RequestMethod == http.MethodPatch {
		if mapData, ok := data.(map[string]interface{}); ok {
			mapData = datareplacer.ReplaceData(params.RequestData, mapData)
		}
	}

	if params.WrapWithList {
		nestedData := map[string]interface{}{
			"data": []interface{}{data},
			"meta": meta,
		}
		return nestedData, nil
	}
	nestedData := map[string]interface{}{
		"data": data,
	}
	return nestedData, nil
}

func (g *DataGenerator) prepareSchemaExample(s *spec.Schema) *valueWrapper {
	if s.Example != nil {
		var fixture interface{}

		if err := json.Unmarshal(s.Example, &fixture); err != nil {
			panic(err)
		}

		return &valueWrapper{value: fixture}
	}

	return nil
}

// generateInternal encompasses all the generation logic. It's separate from
// Generate only so that Generate can seed it with a little bit of information.
func (g *DataGenerator) generateInternal(params *GenerateParams) (interface{}, error) {
	// This is a bit of a mess. We don't have an elegant fully-general approach to
	// generating examples, just a bunch of specific cases that we know how to
	// handle. If we find ourselves in a situation that doesn't match any of the
	// cases, then we fall through to the end of the function and panic().
	// Obviously this is fragile, so we have a unit test that makes sure it works
	// correctly on every resource; hopefully this will at least allow us to catch
	// any errors in advance.

	context := params.context
	schema, err := params.schema.ResolveRef(g.definitions)

	if err != nil {
		return nil, err
	}

	// Determine if the requested expansions are possible
	if params.Expansions != nil && schema.XExpandableFields != nil {
		for key := range params.Expansions.expansions {
			if sort.SearchStrings(*schema.XExpandableFields, key) ==
				len(*schema.XExpandableFields) {
				return nil, errExpansionNotSupported
			}
		}
	}

	example := params.example
	if (example == nil || example.value == nil) && schema.XResourceID != "" {
		// Use the fixture as our example. (Note that if the caller gave us a
		// non-trivial example, we prefer it instead, because it's probably more
		// relevant in context.)
		fixture, ok := g.fixtures.Resources[spec.ResourceID(schema.XResourceID)]
		if !ok {
			panic(fmt.Sprintf("%sMissing fixture for: %s", context, schema.XResourceID))
		}

		example = &valueWrapper{value: fixture}
		context = fmt.Sprintf("%sUsing fixture '%s':\n", context, schema.XResourceID)
	}

	if schema.XExpansionResources != nil {
		if params.Expansions != nil {
			// We're expanding this specific object
			return g.generateInternal(&GenerateParams{
				Expansions:    params.Expansions,
				PathParams:    nil,
				RequestMethod: params.RequestMethod,
				RequestPath:   params.RequestPath,

				schema:  schema.XExpansionResources.OneOf[0],
				context: fmt.Sprintf("%sExpanding optional expandable field:\n", context),
				example: nil,
			})
		}

		// We're not expanding this specific object. Our example should be of
		// the unexpanded form, which is the first branch of the AnyOf
		return g.generateInternal(&GenerateParams{
			Expansions:    params.Expansions,
			PathParams:    nil,
			RequestMethod: params.RequestMethod,
			RequestPath:   params.RequestPath,

			schema:  schema.AnyOf[0],
			context: fmt.Sprintf("%sNot expanding optional expandable field:\n", context),
			example: example,
		})
	}

	if len(schema.AnyOf) == 1 && schema.Nullable {
		if example != nil && example.value == nil {
			if params.Expansions == nil {
				return nil, nil
			}
		} else {
			// Since there's only one subschema, we can confidently recurse into it
			return g.generateInternal(&GenerateParams{
				Expansions:    params.Expansions,
				PathParams:    nil,
				RequestMethod: params.RequestMethod,
				RequestPath:   params.RequestPath,

				schema:  schema.AnyOf[0],
				context: fmt.Sprintf("%sChoosing only branch of anyOf:\n", context),
				example: example,
			})
		}
	}

	if len(schema.AnyOf) != 0 {
		anyOfSchema, err := g.findAnyOfBranch(schema, params.RequestMethod == http.MethodDelete)
		if err != nil {
			return nil, err
		}

		var context string
		if anyOfSchema != nil {
			context = fmt.Sprintf("%sChoosing branch of anyOf based on request method:\n", context)
		} else {
			context = fmt.Sprintf("%sChoosing first branch of anyOf:\n", context)
			anyOfSchema = schema.AnyOf[0]
		}

		// Just generate an example of the first subschema. Note that we don't pass
		// in any example, even if we have an example available, because we don't
		// know which branch of the AnyOf the example corresponds to.
		return g.generateInternal(&GenerateParams{
			Expansions:    params.Expansions,
			PathParams:    nil,
			RequestMethod: params.RequestMethod,
			RequestPath:   params.RequestPath,

			schema:  anyOfSchema,
			context: context,
			example: nil,
		})
	}

    if len(schema.OneOf) > 0 {
        // For OneOf, we always return the fist branch
        return g.generateInternal(&GenerateParams{
            Expansions:    params.Expansions,
            PathParams:    nil,
            RequestMethod: params.RequestMethod,
            RequestPath:   params.RequestPath,

            schema:  schema.OneOf[0],
            context: fmt.Sprintf("%sChoosing first branch of oneOf:\n", context),
            example: example,
        })
    }

	if isListResource(schema) {
		// We special-case list resources and always fill in the list with at least
		// one item of data, regardless of what was present in the example
		listData, err := g.generateListResource(&GenerateParams{
			Expansions:    params.Expansions,
			PathParams:    nil,
			RequestMethod: params.RequestMethod,
			RequestPath:   params.RequestPath,

			schema:  schema,
			context: context,
			example: example,
		})
		return listData, err
	}

	// Generate a synthethic schema as a last ditch effort
	if example == nil && schema.XResourceID == "" {
		example = &valueWrapper{value: g.generateSyntheticFixture(schema, context)}

		context = fmt.Sprintf("%sGenerated synthetic fixture: %+v\n", context, schema)

		if verbose {
			// We list properties here because the schema might not have a
			// better name to identify it with.
			fmt.Printf("Generated synthetic fixture with properties: %s\n",
				stringOrEmpty(propertyNames(schema)))
		}
	}

	if example == nil {
		// If none of the above conditions met, we've run out of ways of generating
		// examples from scratch, so we can only raise an error.
		panic(fmt.Sprintf("%sCannot find or generate example for: %s", context, schema))
	}

	if example.value == nil {
		if params.Expansions != nil {
			panic(fmt.Sprintf("%sWe were asked to expand a key, but our example "+
				"has null for that key.", context))
		}
		return nil, nil
	}

	// XXX: Not sure if this is the best solution for handling enums. Perhaps
	// this should be handled by `isListResource`?
	if len(schema.Enum) != 0 {
		return example.value, nil
	}

	if schema.Type == "boolean" || schema.Type == "integer" ||
		schema.Type == "number" || schema.Type == "string" {
		return example.value, nil
	}

	if schema.Type == "object" && schema.Properties == nil {
		// For a generic object type with no particular properties specified, we
		// assume it must not contain any expandable fields or list resources
		return example.value, nil
	}

	if schema.Type == "array" {
		// For lists that aren't contained in a list-object, we assume they do not
		// contain any expandable fields or list resources
		return example.value, nil
	}

	if (schema.Type == "object" || schema.Type == "") && schema.Properties != nil {
		exampleMap, ok := example.value.(map[string]interface{})
		if !ok {
			panic(fmt.Sprintf(
				"%sSchema is an object:\n%s\n\nBut example is (type: %v):\n%s",
				context, schema, reflect.TypeOf(example.value), example.value))
		}

		resultMap := make(map[string]interface{})

		for key, subSchema := range schema.Properties {
			var subExpansions *ExpansionLevel
			if params.Expansions != nil {
				subExpansions = params.Expansions.expansions[key]
				if subExpansions == nil && params.Expansions.wildcard {
					// No expansion was provided for this key but the wildcard bit is set,
					// so make a fake expansion
					subExpansions = &ExpansionLevel{
						expansions: make(map[string]*ExpansionLevel),
						wildcard:   false,
					}
				}
			}

			var subvalueWrapper *valueWrapper
			subvalueWrapperValue, exampleHasKey := exampleMap[key]
			if exampleHasKey {
				subvalueWrapper = &valueWrapper{value: subvalueWrapperValue}
			}

			if !exampleHasKey && subExpansions == nil {
				// If the example omitted this key, then so do we; unless we were asked
				// to expand the key, in which case we'll have to generate an example
				// from scratch.
				continue
			}

			subValue, err := g.generateInternal(&GenerateParams{
				Expansions:    subExpansions,
				PathParams:    nil,
				RequestMethod: params.RequestMethod,
				RequestPath:   params.RequestPath,

				schema:  subSchema,
				context: fmt.Sprintf("%sIn property '%s' of object:\n", context, key),
				example: subvalueWrapper,
			})
			if err != nil {
				return nil, err
			}
			resultMap[key] = subValue
		}

		return resultMap, nil
	}

	// If the schema is of the format we expect, this shouldn't ever happen.
	panic(fmt.Sprintf(
		"%sEncountered unusual scenario:\nschema=%s\nexample=%+v\n Ensure that the schema has `type: object`",
		context, schema, example))
}

// findAnyOfBranch finds a branch of a schema containing `anyOf` that's either
// a deleted resource or not based off of the value of the deleted argument.
func (g *DataGenerator) findAnyOfBranch(schema *spec.Schema, deleted bool) (*spec.Schema, error) {
	for _, anyOfSchema := range schema.AnyOf {
		anyOfSchema, err := anyOfSchema.ResolveRef(g.definitions)

		if err != nil {
			return nil, err
		}

		deletedResource := isDeletedResource(anyOfSchema)
		if deleted == deletedResource {
			return anyOfSchema, nil
		}
	}
	return nil, nil
}

func (g *DataGenerator) generateListResource(params *GenerateParams) (interface{}, error) {
	var itemExpansions *ExpansionLevel
	if params.Expansions != nil {
		itemExpansions = params.Expansions.expansions["data"]
	}

	itemData, err := g.generateInternal(&GenerateParams{
		Expansions:    itemExpansions,
		PathParams:    nil,
		RequestMethod: params.RequestMethod,
		RequestPath:   params.RequestPath,

		schema:  params.schema.Properties["data"].Items,
		context: fmt.Sprintf("%sPopulating list resource:\n", params.context),
		example: nil,
	})
	if err != nil {
		return nil, err
	}

	// This is written to hopefully be a little more forward compatible in that
	// it respects the list properties dictated by the included schema rather
	// than assuming its own.
	listData := make(map[string]interface{})
	for key, subSchema := range params.schema.Properties {
		var val interface{}
		switch key {
		case "data":
			val = []interface{}{itemData}
		case "has_more":
			val = false
		case "object":
			val = "list"
		case "total_count":
			val = 1
		case "url":
			if strings.HasPrefix(subSchema.Pattern, "^") {
				// Many list resources have a URL pattern of the form "^/v1/whatevers";
				// we cut off the "^" to leave the URL.
				val = subSchema.Pattern[1:]
			} else if params.example != nil {
				// If an example was provided, we can assume it has the correct format
				example := params.example.value.(map[string]interface{})
				val = example["url"].(string)
			} else {
				val = params.RequestPath
			}
		default:
			val = nil
		}
		listData[key] = val
	}
	return listData, nil
}

// generateSyntheticFixture generates a synthetic fixture for the given schema
// by examining its properties and returning default values for each.
//
// This is useful in cases where we don't have a valid fixture for some object.
// That could happen for a prerelease object or in cases where an expansion has
// been requested for an embedded object that doesn't occur at the top level of
// the API.
//
// This function calls itself recursively by initially iterating through every
// property in an object schema, then recursing and returning values for
// embedded objects and scalars.
func (g *DataGenerator) generateSyntheticFixture(schema *spec.Schema, context string) interface{} {
	context = fmt.Sprintf("%sGenerating synthetic fixture: %+v\n", context, schema)

	// Always try to use the user provided example first
	if schema.Example != nil {
		return schema.Example
	}

	// Return the minimum viable object by returning nil/null for a nullable
	// property.
	if schema.Nullable {
		return nil
	}

	if schema.Ref != "" {
		resolved, err := schema.ResolveRef(g.definitions)

		if err != nil {
			panic(err)
		}

		return g.generateSyntheticFixture(resolved, context)
	}

	// Return a member of an enum if one is available because it's probably
	// going to be a more realistic value.
	if len(schema.Enum) > 0 {
		return schema.Enum[0]
	}

	if len(schema.AnyOf) > 0 {
		for _, subSchema := range schema.AnyOf {
			// We don't handle dereferencing here right now, but it's plausible
			if subSchema.Ref != "" {
				continue
			}
			return g.generateSyntheticFixture(subSchema, context)
		}
		panic(fmt.Sprintf("%sCouldn't find an anyOf branch to take", context))
	}

	switch schema.Type {
	case spec.TypeArray:
		return []string{}

	case spec.TypeBoolean:
		return true

	case spec.TypeInteger:
		return 0

	case spec.TypeNumber:
		return 0.0

	case spec.TypeObject:
		fixture := make(map[string]interface{})
		for property, subSchema := range schema.Properties {
			fixture[property] = g.generateSyntheticFixture(subSchema, context)
		}
		return fixture

	case spec.TypeString:
		return ""
	}

	panic(fmt.Sprintf("%sUnhandled type: %s", context, stringOrEmpty(schema.Type)))
}

//
// Private values
//

var errExpansionNotSupported = fmt.Errorf("Expansion not supported")

//
// Private types
//

// valueWrapper wraps an example value that we're generating.
//
// It exists so that we can make a distinction between an example that we don't
// have (where `valueWrapper` itself is `nil`) from one where we have an
// example, but it has a `null` value (where we'd have `valueWrapper{value:
// nil}`).
type valueWrapper struct {
	value interface{}
}

//
// Private functions
//

// distributeReplacedIDs descends through a generated data structure
// recursively looking for IDs that were generated during data generation and
// replaces them with their appropriate replacement value.
func distributeReplacedIDs(pathParams *PathParamsMap, data interface{}) {
	dataSlice, ok := data.([]interface{})
	if ok {
		for _, val := range dataSlice {
			distributeReplacedIDs(pathParams, val)
		}
		return
	}

	dataMap, ok := data.(map[string]interface{})
	if !ok {
		return
	}

	for key, value := range dataMap {
		newValue, ok := distributeReplacedIDsInValue(pathParams, value)
		if ok {
			dataMap[key] = newValue
			continue
		}

		if key == "url" {
			newValue, ok := distributeReplacedIDsInURL(pathParams, value)
			if ok {
				dataMap[key] = newValue
				continue
			}
		}

		distributeReplacedIDs(pathParams, value)
	}
}

// distributeReplacedIDsInValue returns a new value for the `url` field of a
// list object if it's detected that its value contained an ID that we replaced
// with an injected one.
//
// For example, in the URL `/v1/charges/ch_123/refunds`, `ch_123` may have been
// a replaced ID.
func distributeReplacedIDsInURL(pathParams *PathParamsMap, value interface{}) (string, bool) {
	valStr, ok := value.(string)
	if !ok {
		return "", false
	}

	if pathParams.replacedPrimaryID != nil {
		search := "/" + *pathParams.replacedPrimaryID + "/"
		if strings.Index(valStr, search) != -1 {
			return strings.Replace(valStr, search, "/"+*pathParams.PrimaryID+"/", 1), true
		}
	}

	for _, secondaryID := range pathParams.SecondaryIDs {
		for _, replacedID := range secondaryID.replacedIDs {
			search := "/" + replacedID + "/"
			if strings.Index(valStr, search) != -1 {
				return strings.Replace(valStr, search, "/"+secondaryID.ID+"/", 1), true
			}
		}
	}

	return "", false
}

// distributeReplacedIDsInValue returns a new value for an existing one if it's
// detected that its value was an ID that we replaced with an injected one.
//
// It works by comparing the value against any replacement ID values that were
// found in pathParams. Replacement IDs were added to pathParams when the
// generator was doing another pass earlier on in the process.
func distributeReplacedIDsInValue(pathParams *PathParamsMap, value interface{}) (string, bool) {
	valStr, ok := value.(string)
	if !ok {
		return "", false
	}

	if pathParams.replacedPrimaryID != nil && valStr == *pathParams.replacedPrimaryID {
		return *pathParams.PrimaryID, true
	}

	for _, secondaryID := range pathParams.SecondaryIDs {
		for _, replacedID := range secondaryID.replacedIDs {
			if valStr == replacedID {
				return secondaryID.ID, true
			}
		}
	}

	return "", false
}

func isDeletedResource(schema *spec.Schema) bool {
	_, ok := schema.Properties["deleted"]
	return ok
}

func isListResource(schema *spec.Schema) bool {
	if schema.Type != "object" || schema.Properties == nil {
		return false
	}

	object, ok := schema.Properties["object"]
	if !ok || object.Enum == nil || object.Enum[0] != "list" {
		return false
	}

	data, ok := schema.Properties["data"]
	if !ok || data.Items == nil {
		return false
	}

	return true
}

// isRequiredProperty checks whether the given property name is required for
// the given schema. Note that this assumes that the schema is of type object
// because that would be semantic nonsense for any other type.
func isRequiredProperty(schema *spec.Schema, name string) bool {
	for _, property := range schema.Required {
		if name == property {
			return true
		}
	}
	return false
}

// logReplacedID is just a logging shortcut for replaceIDsInternal so that we
// can keep its function body more succinct.
func logReplacedID(prevID, newID string) {
	if !verbose {
		return
	}

	fmt.Printf("Found ID to replace; previous: '%s' new: '%s'\n",
		prevID, newID)
}

// propertyNames returns the names of all properties of a schema joined
// together and comma-separated.
//
// This is useful for printing debugging information.
func propertyNames(schema *spec.Schema) string {
	var names []string
	for name := range schema.Properties {
		names = append(names, name)
	}

	// Sort just so we can have stable output to test against (the order at
	// which keys will be iterated in the map is undefined).
	sort.Strings(names)

	return strings.Join(names, ", ")
}

// recordAndReplaceIDs descends through a generated data structure recursively
// looking for object IDs and replaces them with values from the request's URL
// (i.e., what's in pathParams) where appropriate.
//
// Returns the same PathParamsMap given to it as a parameter, after some
// mutation. It's returned to add clarity as to what's happening to its
// invocation sites.
func recordAndReplaceIDs(pathParams *PathParamsMap, data interface{}) *PathParamsMap {
	recordAndReplaceIDsInternal(pathParams, data, nil, 0)
	return pathParams
}

// recordAndReplaceIDsInternal is identical to recordAndReplaceIDs, but is an
// internal interface that tracks a parent key and recursion level. Use
// recordAndReplaceIDs instead.
func recordAndReplaceIDsInternal(pathParams *PathParamsMap, data interface{},
	parentKey *string, recurseLevel int) {

	dataSlice, ok := data.([]interface{})
	if ok {
		for _, val := range dataSlice {
			recordAndReplaceIDsInternal(pathParams, val, nil, recurseLevel+1)
		}
		return
	}

	dataMap, ok := data.(map[string]interface{})
	if !ok {
		return
	}

	for key, val := range dataMap {
		strVal, ok := val.(string)
		if key == "id" && ok {
			if recurseLevel == 0 {
				// We'll only use a primary ID at the top level of the object
				// (which is why we track recursion level).
				if pathParams.PrimaryID != nil {
					pathParams.replacedPrimaryID = &strVal
					dataMap["id"] = *pathParams.PrimaryID
					logReplacedID(strVal, *pathParams.PrimaryID)
				}
			} else {
				// After the object's top level, we'll replace an object's ID
				// if either of these two values are the same s the secondary
				// ID's name (i.e., the "name" for the parameter that was
				// extracted from the path in OpenAPI):
				//
				// (1) The value in the object's `object` field.
				// (2) The value of the object's parent key (e.g., say it's a
				//     "charge" object that was nested under a refund's
				//     `charge` key).
				objectVal, ok := dataMap["object"].(string)
				if ok {
					for _, secondaryID := range pathParams.SecondaryIDs {
						if objectVal == secondaryID.Name {
							secondaryID.appendReplacedID(strVal)
							dataMap["id"] = secondaryID.ID
							logReplacedID(strVal, secondaryID.ID)
							break
						}
					}
				}

				for _, secondaryID := range pathParams.SecondaryIDs {
					if parentKey != nil && *parentKey == secondaryID.Name {
						secondaryID.appendReplacedID(strVal)
						dataMap["id"] = secondaryID.ID
						logReplacedID(strVal, secondaryID.ID)
						break
					}
				}
			}
		} else {
			if ok {
				// This path replaces a string value with a secondary ID if the
				// name of the field matches the secondary ID's target name.
				//
				// For example, an application fee refund might have an
				// embedded `fee` field which is the ID of its parent
				// application fee (unless it's expanded, at which point it
				// will be handled by the case above).
				for _, secondaryID := range pathParams.SecondaryIDs {
					if key == secondaryID.Name {
						secondaryID.appendReplacedID(strVal)
						dataMap[key] = secondaryID.ID
						logReplacedID(strVal, secondaryID.ID)
						break
					}
				}
			} else {
				recordAndReplaceIDsInternal(pathParams, val, &key, recurseLevel+1)
			}
		}
	}
}

// stringOrEmpty returns the string given as parameter, or the string "(empty)"
// if the string was empty.
//
// This is useful in cases like logging to make sure that something is always
// printed on screen (instead of a strangely truncated sentence for an empty
// value).
func stringOrEmpty(s string) string {
	if s == "" {
		return "(empty)"
	}
	return s
}
