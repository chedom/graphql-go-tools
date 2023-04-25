package sdlmerge

import (
	"github.com/wundergraph/graphql-go-tools/pkg/ast"
	"github.com/wundergraph/graphql-go-tools/pkg/astvisitor"
	"github.com/wundergraph/graphql-go-tools/pkg/operationreport"
)

type validateFieldVisitor struct {
	*astvisitor.Walker
	document          *ast.Document
	sharedTypeSet     map[string]fieldedSharedType
	rootNodesToRemove []ast.Node
	lastInputRef      int
	lastInterfaceRef  int
	lastObjectRef     int
}

func newValidateFieldVisitor() *validateFieldVisitor {
	return &validateFieldVisitor{
		nil,
		nil,
		make(map[string]fieldedSharedType),
		nil,
		ast.InvalidRef,
		ast.InvalidRef,
		ast.InvalidRef,
	}
}

func (v *validateFieldVisitor) Register(walker *astvisitor.Walker) {
	v.Walker = walker
	walker.RegisterEnterDocumentVisitor(v)
	walker.RegisterEnterInputObjectTypeDefinitionVisitor(v)
	walker.RegisterEnterInterfaceTypeDefinitionVisitor(v)
	walker.RegisterEnterObjectTypeDefinitionVisitor(v)
	walker.RegisterEnterObjectTypeExtensionVisitor(v)
}

func (v *validateFieldVisitor) EnterDocument(operation, _ *ast.Document) {
	v.document = operation
}

func (v *validateFieldVisitor) EnterInputObjectTypeDefinition(ref int) {
	if ref <= v.lastObjectRef {
		return
	}
	name := v.document.InputObjectTypeDefinitionNameString(ref)
	refs := v.document.InputObjectTypeDefinitions[ref].InputFieldsDefinition.Refs
	input, exists := v.sharedTypeSet[name]
	if exists {
		if !input.compareFields(refs) {
			v.StopWithExternalErr(operationreport.ErrSharedTypesMustBeIdenticalToFederate(name))
			return
		}
	} else {
		v.sharedTypeSet[name] = newFieldedSharedType(v.document, ast.NodeKindInputValueDefinition, refs)
	}
}

func (v *validateFieldVisitor) EnterInterfaceTypeDefinition(ref int) {
	if ref <= v.lastObjectRef {
		return
	}
	name := v.document.InterfaceTypeDefinitionNameString(ref)
	interfaceType := v.document.InterfaceTypeDefinitions[ref]
	refs := interfaceType.FieldsDefinition.Refs
	iFace, exists := v.sharedTypeSet[name]
	if exists {
		if !iFace.compareFields(refs) {
			v.StopWithExternalErr(operationreport.ErrSharedTypesMustBeIdenticalToFederate(name))
			return
		}
	} else {
		v.sharedTypeSet[name] = newFieldedSharedType(v.document, ast.NodeKindFieldDefinition, refs)
	}
}

func (v *validateFieldVisitor) EnterObjectTypeDefinition(ref int) {
	name := v.document.ObjectTypeDefinitionNameString(ref)
	objectType := v.document.ObjectTypeDefinitions[ref]
	refs := objectType.FieldsDefinition.Refs
	object, exists := v.sharedTypeSet[name]
	if exists {
		if !object.compareFields(refs) {
			v.StopWithExternalErr(operationreport.ErrSharedTypesMustBeIdenticalToFederate(name))
			return
		}
	} else {
		v.sharedTypeSet[name] = newFieldedSharedType(v.document, ast.NodeKindFieldDefinition, refs)
	}
}

func (v *validateFieldVisitor) EnterObjectTypeExtension(ref int) {
	name := v.document.ObjectTypeExtensionDescriptionNameString(ref)
	objectType := v.document.ObjectTypeExtensions[ref]
	refs := objectType.FieldsDefinition.Refs
	object, exists := v.sharedTypeSet[name]
	if exists {
		if !object.compareFields(refs) {
			v.StopWithExternalErr(operationreport.ErrSharedTypesMustBeIdenticalToFederate(name))
			return
		}
	} else {
		v.sharedTypeSet[name] = newFieldedSharedType(v.document, ast.NodeKindFieldDefinition, refs)
	}
}
