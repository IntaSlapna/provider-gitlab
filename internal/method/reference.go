/*
Copyright 2021 The Crossplane Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package method

import (
	"go/types"
	"strings"

	"github.com/pkg/errors"

	"github.com/crossplane/crossplane-tools/internal/comments"

	"github.com/dave/jennifer/jen"
)

const (
	ReferenceTypeMarker               = "crossplane:generate:reference:type"
	ReferenceExtractorMarker          = "crossplane:generate:reference:extractor"
	ReferenceReferenceFieldNameMarker = "crossplane:generate:reference:refFieldName"
	ReferenceSelectorFieldNameMarker  = "crossplane:generate:reference:selectorFieldName"
)

// NewResolveReferences returns a NewMethod that writes a SetProviderConfigReference
// method for the supplied Object to the supplied file.
func NewResolveReferences(comm comments.Comments, receiver, clientPath, referencePath string) New {
	return func(f *jen.File, o types.Object) {
		n, ok := o.Type().(*types.Named)
		if !ok {
			return
		}
		defaultExtractor := jen.Qual(referencePath, "ExternalName").Call()
		rs := NewReferenceSearcher(comm, defaultExtractor)
		refs, err := rs.Search(n)
		if err != nil {
			panic(errors.Wrapf(err, "cannot search for references of %s", n.Obj().Name()))
		}
		if len(refs) == 0 {
			return
		}
		hasMultiResolution := false
		hasSingleResolution := false
		resolverCalls := make(jen.Statement, len(refs))
		for i, ref := range refs {
			if ref.IsList {
				hasMultiResolution = true
			} else {
				hasSingleResolution = true
			}
			currentValuePath := jen.Id(receiver)
			for _, fieldName := range strings.Split(ref.GoValueFieldPath, ".") {
				currentValuePath = currentValuePath.Dot(fieldName)
			}
			referenceFieldPath := jen.Id(receiver)
			for _, fieldName := range strings.Split(ref.GoRefFieldPath, ".") {
				referenceFieldPath = referenceFieldPath.Dot(fieldName)
			}
			selectorFieldPath := jen.Id(receiver)
			for _, fieldName := range strings.Split(ref.GoSelectorFieldPath, ".") {
				selectorFieldPath = selectorFieldPath.Dot(fieldName)
			}
			var code *jen.Statement
			if ref.IsList {
				code = &jen.Statement{
					jen.List(jen.Id("mrsp"), jen.Err()).Op("=").Id("r").Dot("ResolveMultiple").Call(
						jen.Id("ctx"),
						jen.Qual(referencePath, "MultiResolutionRequest").Values(jen.Dict{
							jen.Id("CurrentValues"): currentValuePath,
							jen.Id("References"):    referenceFieldPath,
							jen.Id("Selector"):      selectorFieldPath,
							jen.Id("To"): jen.Qual(referencePath, "To").Values(jen.Dict{
								jen.Id("Managed"): ref.RemoteType,
								jen.Id("List"):    ref.RemoteListType,
							}),
							jen.Id("Extract"): ref.Extractor,
						},
						),
					),
					jen.Line(),
					jen.If(jen.Err().Op("!=").Nil()).Block(
						jen.Return(jen.Qual("github.com/pkg/errors", "Wrapf").Call(jen.Err(), jen.Lit(ref.GoValueFieldPath))),
					),
					jen.Line(),
					currentValuePath.Clone().Op("=").Id("mrsp").Dot("ResolvedValues"),
					jen.Line(),
					referenceFieldPath.Clone().Op("=").Id("mrsp").Dot("ResolvedReferences"),
					jen.Line(),
				}
			} else {
				setResolvedValue := currentValuePath.Clone().Op("=").Id("rsp").Dot("ResolvedValue")
				if ref.IsPointer {
					setResolvedValue = currentValuePath.Clone().Op("=").Qual(referencePath, "ToPtrValue").Call(jen.Id("rsp").Dot("ResolvedValue"))
					currentValuePath = jen.Qual(referencePath, "FromPtrValue").Call(currentValuePath)
				}
				code = &jen.Statement{
					jen.List(jen.Id("rsp"), jen.Err()).Op("=").Id("r").Dot("Resolve").Call(
						jen.Id("ctx"),
						jen.Qual(referencePath, "ResolutionRequest").Values(jen.Dict{
							jen.Id("CurrentValue"): currentValuePath,
							jen.Id("Reference"):    referenceFieldPath,
							jen.Id("Selector"):     selectorFieldPath,
							jen.Id("To"): jen.Qual(referencePath, "To").Values(jen.Dict{
								jen.Id("Managed"): ref.RemoteType,
								jen.Id("List"):    ref.RemoteListType,
							}),
							jen.Id("Extract"): ref.Extractor,
						},
						),
					),
					jen.Line(),
					jen.If(jen.Err().Op("!=").Nil()).Block(
						jen.Return(jen.Qual("github.com/pkg/errors", "Wrapf").Call(jen.Err(), jen.Lit(ref.GoValueFieldPath))),
					),
					jen.Line(),
					setResolvedValue,
					jen.Line(),
					referenceFieldPath.Clone().Op("=").Id("rsp").Dot("ResolvedReference"),
					jen.Line(),
				}
			}
			resolverCalls[i] = code
		}
		var initStatements jen.Statement
		if hasSingleResolution {
			initStatements = append(initStatements, jen.Var().Id("rsp").Qual(referencePath, "ResolutionResponse"), jen.Line())
		}
		if hasMultiResolution {
			initStatements = append(initStatements, jen.Var().Id("mrsp").Qual(referencePath, "MultiResolutionResponse"))
		}

		f.Commentf("ResolveReferences of this %s.", o.Name())
		f.Func().Params(jen.Id(receiver).Op("*").Id(o.Name())).Id("ResolveReferences").
			Params(
				jen.Id("ctx").Qual("context", "Context"),
				jen.Id("c").Qual(clientPath, "Reader"),
			).Error().Block(
			jen.Id("r").Op(":=").Qual(referencePath, "NewAPIResolver").Call(jen.Id("c"), jen.Id(receiver)),
			jen.Line(),
			&initStatements,
			jen.Var().Err().Error(),
			jen.Line(),
			&resolverCalls,
			jen.Line(),
			jen.Return(jen.Nil()),
		)
	}
}

// Target type string

type Reference struct {
	RemoteType *jen.Statement
	Extractor  *jen.Statement

	RemoteListType      *jen.Statement
	GoValueFieldPath    string
	GoRefFieldPath      string
	GoSelectorFieldPath string
	IsList              bool
	IsPointer           bool
}

func NewReferenceSearcher(comm comments.Comments, defaultExtractor *jen.Statement) *ReferenceSearcher {
	return &ReferenceSearcher{
		Comments:         comm,
		DefaultExtractor: defaultExtractor,
	}
}

type ReferenceSearcher struct {
	Comments         comments.Comments
	DefaultExtractor *jen.Statement

	refs []Reference
}

func (rs *ReferenceSearcher) Search(n *types.Named) ([]Reference, error) {
	return rs.refs, errors.Wrap(rs.search(n), "search for references failed")
}

func (rs *ReferenceSearcher) search(n *types.Named, fields ...string) error {
	s, ok := n.Underlying().(*types.Struct)
	if !ok {
		return nil
	}

	for i := 0; i < s.NumFields(); i++ {
		field := s.Field(i)
		isPointer := false
		isList := false
		switch ft := field.Type().(type) {
		// Type
		case *types.Named:
			if err := rs.search(ft, append(fields, field.Name())...); err != nil {
				return errors.Wrapf(err, "cannot search for references in %s", ft.Obj().Name())
			}
		// *Type
		case *types.Pointer:
			isPointer = true
			switch elemType := ft.Elem().(type) {
			case *types.Named:
				if err := rs.search(elemType, append(fields, "*"+field.Name())...); err != nil {
					return errors.Wrapf(err, "cannot search for references in %s", elemType.Obj().Name())
				}
			}
		case *types.Slice:
			isList = true
			switch elemType := ft.Elem().(type) {
			// []Type
			case *types.Named:
				if err := rs.search(elemType, append(fields, "[]"+field.Name())...); err != nil {
					return errors.Wrapf(err, "cannot search for references in %s", elemType.Obj().Name())
				}
				// There could be []*Type but we don't support if for now.
			}
		}
		markers := comments.ParseMarkers(rs.Comments.For(field))
		refTypeValues := markers[ReferenceTypeMarker]
		if len(refTypeValues) == 0 {
			continue
		}
		refType := refTypeValues[0]

		extractorValues := markers[ReferenceExtractorMarker]
		extractorPath := rs.DefaultExtractor
		if len(extractorValues) != 0 {
			extractorPath = getTypeCodeFromPath(extractorValues[0])
		}
		fieldPath := strings.Join(append(fields, field.Name()), ".")
		rs.refs = append(rs.refs, Reference{
			RemoteType:          getTypeCodeFromPath(refType),
			RemoteListType:      getTypeCodeFromPath(refType, "List"),
			Extractor:           extractorPath,
			GoValueFieldPath:    fieldPath,
			GoRefFieldPath:      getRefFieldName(markers, fieldPath, isList),
			GoSelectorFieldPath: getSelectorFieldName(markers, fieldPath),
			IsPointer:           isPointer,
			IsList:              isList,
		})
	}
	return nil
}

func getRefFieldName(markers comments.Markers, valueFieldPath string, isList bool) string {
	if vals, ok := markers[ReferenceReferenceFieldNameMarker]; ok {
		f := strings.Split(valueFieldPath, ".")
		return strings.Join(f[:len(f)-1], ".") + "." + vals[0]
	}
	if isList {
		return valueFieldPath + "Refs"
	}
	return valueFieldPath + "Ref"
}

func getSelectorFieldName(markers comments.Markers, valueFieldPath string) string {
	if vals, ok := markers[ReferenceSelectorFieldNameMarker]; ok {
		f := strings.Split(valueFieldPath, ".")
		return strings.Join(f[:len(f)-1], ".") + "." + vals[0]
	}
	return valueFieldPath + "Selector"
}

func getTypeCodeFromPath(path string, nameSuffix ...string) *jen.Statement {
	words := strings.Split(path, ".")
	if len(words) == 1 {
		return jen.Op("&").Id(path + strings.Join(nameSuffix, "")).Values()
	}
	name := words[len(words)-1] + strings.Join(nameSuffix, "")
	pkg := strings.TrimSuffix(path, "."+words[len(words)-1])
	return jen.Op("&").Qual(pkg, name).Values()
}
