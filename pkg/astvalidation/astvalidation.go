//go:generate stringer -type=ValidationState -output astvalidation_string.go
package astvalidation

import (
	"bytes"
	"fmt"
	"github.com/jensneuse/graphql-go-tools/pkg/ast"
	"github.com/jensneuse/graphql-go-tools/pkg/astvisitor"
	"github.com/jensneuse/graphql-go-tools/pkg/lexer/literal"
	"github.com/jensneuse/graphql-go-tools/pkg/operationreport"
)

func DefaultOperationValidator() *OperationValidator {

	validator := OperationValidator{
		walker: astvisitor.NewWalker(48),
	}

	validator.RegisterRule(OperationNameUniqueness())
	validator.RegisterRule(LoneAnonymousOperation())
	validator.RegisterRule(SubscriptionSingleRootField())
	validator.RegisterRule(FieldSelections())
	validator.RegisterRule(FieldSelectionMerging())
	validator.RegisterRule(ValidArguments())
	validator.RegisterRule(Values())
	validator.RegisterRule(ArgumentUniqueness())
	validator.RegisterRule(RequiredArguments())
	validator.RegisterRule(Fragments())
	validator.RegisterRule(DirectivesAreDefined())
	validator.RegisterRule(DirectivesAreInValidLocations())
	validator.RegisterRule(VariableUniqueness())
	validator.RegisterRule(DirectivesAreUniquePerLocation())
	validator.RegisterRule(VariablesAreInputTypes())
	validator.RegisterRule(AllVariableUsesDefined())
	validator.RegisterRule(AllVariablesUsed())

	return &validator
}

type ValidationState int

const (
	UnknownState ValidationState = iota
	Valid
	Invalid
)

type Rule func(walker *astvisitor.Walker)

type OperationValidator struct {
	walker astvisitor.Walker
}

func (o *OperationValidator) RegisterRule(rule Rule) {
	rule(&o.walker)
}

func (o *OperationValidator) Validate(operation, definition *ast.Document, report *operationreport.Report) ValidationState {

	if report == nil {
		report = &operationreport.Report{}
	}

	o.walker.Walk(operation, definition, report)

	if report.HasErrors() {
		return Invalid
	}
	return Valid
}

func OperationNameUniqueness() Rule {
	return func(walker *astvisitor.Walker) {
		walker.RegisterEnterDocumentVisitor(&operationNameUniquenessVisitor{walker})
	}
}

type operationNameUniquenessVisitor struct {
	*astvisitor.Walker
}

func (o *operationNameUniquenessVisitor) EnterDocument(operation, definition *ast.Document) {
	if len(operation.OperationDefinitions) <= 1 {
		return
	}

	for i := range operation.OperationDefinitions {
		for k := range operation.OperationDefinitions {
			if i == k || i > k {
				continue
			}

			left := operation.OperationDefinitions[i].Name
			right := operation.OperationDefinitions[k].Name

			if ast.ByteSliceEquals(left, operation.Input, right, operation.Input) {
				operationName := operation.Input.ByteSlice(operation.OperationDefinitions[i].Name)
				o.StopWithExternalErr(operationreport.ErrOperationNameMustBeUnique(operationName))
				return
			}
		}
	}
}

func LoneAnonymousOperation() Rule {
	return func(walker *astvisitor.Walker) {
		walker.RegisterEnterDocumentVisitor(&loneAnonymousOperationVisitor{walker})
	}
}

type loneAnonymousOperationVisitor struct {
	*astvisitor.Walker
}

func (l *loneAnonymousOperationVisitor) EnterDocument(operation, definition *ast.Document) {
	if len(operation.OperationDefinitions) <= 1 {
		return
	}

	for i := range operation.OperationDefinitions {
		if operation.OperationDefinitions[i].Name.Length() == 0 {
			l.StopWithExternalErr(operationreport.ErrAnonymousOperationMustBeTheOnlyOperationInDocument())
			return
		}
	}
}

func SubscriptionSingleRootField() Rule {
	return func(walker *astvisitor.Walker) {
		visitor := subscriptionSingleRootFieldVisitor{walker}
		walker.RegisterEnterDocumentVisitor(&visitor)
	}
}

type subscriptionSingleRootFieldVisitor struct {
	*astvisitor.Walker
}

func (s *subscriptionSingleRootFieldVisitor) EnterDocument(operation, definition *ast.Document) {
	for i := range operation.OperationDefinitions {
		if operation.OperationDefinitions[i].OperationType == ast.OperationTypeSubscription {
			selections := len(operation.SelectionSets[operation.OperationDefinitions[i].SelectionSet].SelectionRefs)
			if selections > 1 {
				subscriptionName := operation.Input.ByteSlice(operation.OperationDefinitions[i].Name)
				s.StopWithExternalErr(operationreport.ErrSubscriptionMustOnlyHaveOneRootSelection(subscriptionName))
				return
			} else if selections == 1 {
				ref := operation.SelectionSets[operation.OperationDefinitions[i].SelectionSet].SelectionRefs[0]
				if operation.Selections[ref].Kind == ast.SelectionKindField {
					return
				}
			}
		}
	}
}

func FieldSelections() Rule {
	return func(walker *astvisitor.Walker) {
		fieldDefined := fieldDefined{
			Walker: walker,
		}
		walker.RegisterEnterDocumentVisitor(&fieldDefined)
		walker.RegisterEnterFieldVisitor(&fieldDefined)
	}
}

type fieldDefined struct {
	*astvisitor.Walker
	operation  *ast.Document
	definition *ast.Document
}

func (f *fieldDefined) EnterDocument(operation, definition *ast.Document) {
	f.operation = operation
	f.definition = definition
}

func (f *fieldDefined) ValidateUnionField(ref int, enclosingTypeDefinition ast.Node) {
	if bytes.Equal(f.operation.FieldNameBytes(ref), literal.TYPENAME) {
		return
	}
	fieldName := f.operation.FieldNameBytes(ref)
	unionName := f.definition.NodeNameBytes(enclosingTypeDefinition)
	f.StopWithExternalErr(operationreport.ErrFieldSelectionOnUnion(fieldName, unionName))
}

func (f *fieldDefined) ValidateInterfaceObjectTypeField(ref int, enclosingTypeDefinition ast.Node) {
	fieldName := f.operation.FieldNameBytes(ref)
	typeName := f.definition.NodeNameBytes(enclosingTypeDefinition)
	hasSelections := f.operation.FieldHasSelections(ref)
	definitions := f.definition.NodeFieldDefinitions(enclosingTypeDefinition)
	for _, i := range definitions {
		definitionName := f.definition.FieldDefinitionNameBytes(i)
		if bytes.Equal(fieldName, definitionName) {
			// field is defined
			fieldDefinitionTypeKind := f.definition.FieldDefinitionTypeNode(i).Kind
			switch {
			case hasSelections && fieldDefinitionTypeKind == ast.NodeKindScalarTypeDefinition:
				f.StopWithExternalErr(operationreport.ErrFieldSelectionOnScalar(fieldName, definitionName))
			case !hasSelections && fieldDefinitionTypeKind != ast.NodeKindScalarTypeDefinition:
				f.StopWithExternalErr(operationreport.ErrMissingFieldSelectionOnNonScalar(fieldName, typeName))
			}
			return
		}
	}

	f.StopWithExternalErr(operationreport.ErrFieldUndefinedOnType(fieldName, typeName))
}

func (f *fieldDefined) ValidateScalarField(ref int, enclosingTypeDefinition ast.Node) {
	fieldName := f.operation.FieldNameBytes(ref)
	scalarTypeName := f.operation.NodeNameBytes(enclosingTypeDefinition)
	f.StopWithExternalErr(operationreport.ErrFieldSelectionOnScalar(fieldName, scalarTypeName))
}

func (f *fieldDefined) EnterField(ref int) {
	switch f.EnclosingTypeDefinition.Kind {
	case ast.NodeKindUnionTypeDefinition:
		f.ValidateUnionField(ref, f.EnclosingTypeDefinition)
	case ast.NodeKindInterfaceTypeDefinition, ast.NodeKindObjectTypeDefinition:
		f.ValidateInterfaceObjectTypeField(ref, f.EnclosingTypeDefinition)
	case ast.NodeKindScalarTypeDefinition:
		f.ValidateScalarField(ref, f.EnclosingTypeDefinition)
	default:
		fieldName := f.operation.FieldNameBytes(ref)
		typeName := f.operation.NodeNameBytes(f.EnclosingTypeDefinition)
		f.StopWithInternalErr(fmt.Errorf("astvalidation/fieldDefined/EnterField: field: %s selection on type: %s unhandled", fieldName, typeName))
	}
}

func FieldSelectionMerging() Rule {
	return func(walker *astvisitor.Walker) {
		visitor := fieldSelectionMergingVisitor{Walker: walker}
		walker.RegisterEnterDocumentVisitor(&visitor)
		walker.RegisterEnterSelectionSetVisitor(&visitor)
		walker.RegisterEnterFieldVisitor(&visitor)
		walker.RegisterEnterOperationVisitor(&visitor)
		walker.RegisterEnterFragmentDefinitionVisitor(&visitor)
	}
}

type fieldSelectionMergingVisitor struct {
	*astvisitor.Walker
	definition, operation *ast.Document
	scalarRequirements    scalarRequirements
	nonScalarRequirements nonScalarRequirements
	refs                  []int
	enclosingTypeRef      int
}
type nonScalarRequirement struct {
	path                    ast.Path
	objectName              ast.ByteSlice
	fieldTypeRef            int
	fieldTypeDefinitionNode ast.Node
}

type nonScalarRequirements []nonScalarRequirement

func (f *fieldSelectionMergingVisitor) NonScalarRequirementsByPathField(path ast.Path, objectName ast.ByteSlice) []int {
	f.refs = f.refs[:0]
	for i := range f.nonScalarRequirements {
		if f.nonScalarRequirements[i].path.Equals(path) && f.nonScalarRequirements[i].objectName.Equals(objectName) {
			f.refs = append(f.refs, i)
		}
	}
	return f.refs
}

type scalarRequirement struct {
	path                    ast.Path
	objectName              ast.ByteSlice
	fieldRef                int
	fieldType               int
	enclosingTypeDefinition ast.Node
	fieldTypeDefinitionNode ast.Node
}

type scalarRequirements []scalarRequirement

func (f *fieldSelectionMergingVisitor) ScalarRequirementsByPathField(path ast.Path, objectName ast.ByteSlice) []int {
	f.refs = f.refs[:0]
	for i := range f.scalarRequirements {
		if f.scalarRequirements[i].path.Equals(path) && f.scalarRequirements[i].objectName.Equals(objectName) {
			f.refs = append(f.refs, i)
		}
	}
	return f.refs
}

func (f *fieldSelectionMergingVisitor) resetRequirements() {
	f.scalarRequirements = f.scalarRequirements[:0]
	f.nonScalarRequirements = f.nonScalarRequirements[:0]
}

func (f *fieldSelectionMergingVisitor) EnterDocument(operation, definition *ast.Document) {
	f.operation = operation
	f.definition = definition
}

func (f *fieldSelectionMergingVisitor) EnterFragmentDefinition(ref int) {
	f.resetRequirements()
}

func (f *fieldSelectionMergingVisitor) EnterOperationDefinition(ref int) {
	f.resetRequirements()
}

func (f *fieldSelectionMergingVisitor) EnterField(ref int) {

	fieldName := f.operation.FieldNameBytes(ref)
	objectName := f.operation.FieldObjectNameBytes(ref)
	definition, ok := f.definition.NodeFieldDefinitionByName(f.EnclosingTypeDefinition, fieldName)
	if !ok {
		enclosingTypeName := f.definition.NodeNameBytes(f.EnclosingTypeDefinition)
		f.StopWithExternalErr(operationreport.ErrFieldUndefinedOnType(fieldName, enclosingTypeName))
		return
	}

	fieldType := f.definition.FieldDefinitionType(definition)
	fieldDefinitionTypeNode := f.definition.FieldDefinitionTypeNode(definition)
	if fieldDefinitionTypeNode.Kind != ast.NodeKindScalarTypeDefinition {

		matchedRequirements := f.NonScalarRequirementsByPathField(f.Path, objectName)
		fieldDefinitionTypeKindPresentInRequirements := false
		for _, i := range matchedRequirements {

			if !f.potentiallySameObject(fieldDefinitionTypeNode, f.nonScalarRequirements[i].fieldTypeDefinitionNode) {
				if !objectName.Equals(f.nonScalarRequirements[i].objectName) {
					f.StopWithExternalErr(operationreport.ErrResponseOfDifferingTypesMustBeOfSameShape(objectName, f.nonScalarRequirements[i].objectName))
					return
				}
			} else if !f.definition.TypesAreCompatibleDeep(f.nonScalarRequirements[i].fieldTypeRef, fieldType) {
				left, err := f.definition.PrintTypeBytes(f.nonScalarRequirements[i].fieldTypeRef, nil)
				if err != nil {
					f.StopWithInternalErr(err)
					return
				}
				right, err := f.definition.PrintTypeBytes(fieldType, nil)
				if err != nil {
					f.StopWithInternalErr(err)
					return
				}
				f.StopWithExternalErr(operationreport.ErrTypesForFieldMismatch(objectName, left, right))
				return
			}

			if fieldDefinitionTypeNode.Kind != f.nonScalarRequirements[i].fieldTypeDefinitionNode.Kind {
				fieldDefinitionTypeKindPresentInRequirements = true
			}
		}

		if len(matchedRequirements) != 0 && fieldDefinitionTypeKindPresentInRequirements {
			return
		}

		f.nonScalarRequirements = append(f.nonScalarRequirements, nonScalarRequirement{
			path:                    f.Path,
			objectName:              objectName,
			fieldTypeRef:            fieldType,
			fieldTypeDefinitionNode: fieldDefinitionTypeNode,
		})

		return
	}

	matchedRequirements := f.ScalarRequirementsByPathField(f.Path, objectName)
	fieldDefinitionTypeKindPresentInRequirements := false

	for _, i := range matchedRequirements {
		if f.potentiallySameObject(f.scalarRequirements[i].enclosingTypeDefinition, f.EnclosingTypeDefinition) {
			if !f.operation.FieldsAreEqualFlat(f.scalarRequirements[i].fieldRef, ref) {
				f.StopWithExternalErr(operationreport.ErrDifferingFieldsOnPotentiallySameType(objectName))
				return
			}
		}
		if !f.definition.TypesAreCompatibleDeep(f.scalarRequirements[i].fieldType, fieldType) {
			left, err := f.definition.PrintTypeBytes(f.scalarRequirements[i].fieldType, nil)
			if err != nil {
				f.StopWithInternalErr(err)
				return
			}
			right, err := f.definition.PrintTypeBytes(fieldType, nil)
			if err != nil {
				f.StopWithInternalErr(err)
				return
			}
			f.StopWithExternalErr(operationreport.ErrFieldsConflict(objectName, left, right))
			return
		}

		if fieldDefinitionTypeNode.Kind != f.scalarRequirements[i].fieldTypeDefinitionNode.Kind {
			fieldDefinitionTypeKindPresentInRequirements = true
		}
	}

	if len(matchedRequirements) != 0 && fieldDefinitionTypeKindPresentInRequirements {
		return
	}

	f.scalarRequirements = append(f.scalarRequirements, scalarRequirement{
		path:                    f.Path,
		objectName:              objectName,
		fieldRef:                ref,
		fieldType:               fieldType,
		enclosingTypeDefinition: f.EnclosingTypeDefinition,
		fieldTypeDefinitionNode: fieldDefinitionTypeNode,
	})
}

func (f *fieldSelectionMergingVisitor) potentiallySameObject(left, right ast.Node) bool {
	switch {
	case left.Kind == ast.NodeKindInterfaceTypeDefinition || right.Kind == ast.NodeKindInterfaceTypeDefinition:
		return true
	case left.Kind == ast.NodeKindObjectTypeDefinition && right.Kind == ast.NodeKindObjectTypeDefinition:
		return bytes.Equal(f.definition.ObjectTypeDefinitionNameBytes(left.Ref), f.definition.ObjectTypeDefinitionNameBytes(right.Ref))
	default:
		return false
	}
}

func (f *fieldSelectionMergingVisitor) EnterSelectionSet(ref int) {

}

func ValidArguments() Rule {
	return func(walker *astvisitor.Walker) {
		visitor := validArgumentsVisitor{
			Walker: walker,
		}
		walker.RegisterEnterDocumentVisitor(&visitor)
		walker.RegisterEnterArgumentVisitor(&visitor)
	}
}

type validArgumentsVisitor struct {
	*astvisitor.Walker
	operation, definition *ast.Document
}

func (v *validArgumentsVisitor) EnterDocument(operation, definition *ast.Document) {
	v.operation = operation
	v.definition = definition
}

func (v *validArgumentsVisitor) EnterArgument(ref int) {

	definition, exists := v.ArgumentInputValueDefinition(ref)

	if !exists {
		argumentName := v.operation.ArgumentNameBytes(ref)
		ancestorName := v.AncestorNameBytes()
		v.StopWithExternalErr(operationreport.ErrArgumentNotDefinedOnNode(argumentName, ancestorName))
		return
	}

	value := v.operation.ArgumentValue(ref)
	v.validateIfValueSatisfiesInputFieldDefinition(value, definition)
}

func (v *validArgumentsVisitor) validateIfValueSatisfiesInputFieldDefinition(value ast.Value, inputValueDefinition int) {

	var satisfied bool

	switch value.Kind {
	case ast.ValueKindVariable:
		satisfied = v.variableValueSatisfiesInputValueDefinition(value.Ref, inputValueDefinition)
	case ast.ValueKindEnum:
		satisfied = v.enumValueSatisfiesInputValueDefinition(value.Ref, inputValueDefinition)
	case ast.ValueKindNull:
		satisfied = v.nullValueSatisfiesInputValueDefinition(inputValueDefinition)
	case ast.ValueKindBoolean:
		satisfied = v.booleanValueSatisfiesInputValueDefinition(inputValueDefinition)
	case ast.ValueKindInteger:
		satisfied = v.intValueSatisfiesInputValueDefinition(value, inputValueDefinition)
	case ast.ValueKindObject, ast.ValueKindList:
		// object- and list values are covered by Values() / valuesVisitor
		return
	default:
		v.StopWithInternalErr(fmt.Errorf("validateIfValueSatisfiesInputFieldDefinition: not implemented for value.Kind: %s", value.Kind))
		return
	}

	if satisfied {
		return
	}

	printedValue, err := v.operation.PrintValueBytes(value, nil)
	if v.HandleInternalErr(err) {
		return
	}

	typeRef := v.definition.InputValueDefinitionType(inputValueDefinition)

	printedType, err := v.definition.PrintTypeBytes(typeRef, nil)
	if v.HandleInternalErr(err) {
		return
	}

	v.StopWithExternalErr(operationreport.ErrValueDoesntSatisfyInputValueDefinition(printedValue, printedType))
}

func (v *validArgumentsVisitor) intValueSatisfiesInputValueDefinition(value ast.Value, inputValueDefinition int) bool {
	inputType := v.definition.Types[v.definition.InputValueDefinitionType(inputValueDefinition)]
	if inputType.TypeKind == ast.TypeKindNonNull {
		inputType = v.definition.Types[inputType.OfType]
	}
	if inputType.TypeKind != ast.TypeKindNamed {
		return false
	}
	if !bytes.Equal(v.definition.Input.ByteSlice(inputType.Name), literal.INT) {
		return false
	}
	return true
}

func (v *validArgumentsVisitor) booleanValueSatisfiesInputValueDefinition(inputValueDefinition int) bool {
	inputType := v.definition.Types[v.definition.InputValueDefinitionType(inputValueDefinition)]
	if inputType.TypeKind == ast.TypeKindNonNull {
		inputType = v.definition.Types[inputType.OfType]
	}
	if inputType.TypeKind != ast.TypeKindNamed {
		return false
	}
	if !bytes.Equal(v.definition.Input.ByteSlice(inputType.Name), literal.BOOLEAN) {
		return false
	}
	return true
}

func (v *validArgumentsVisitor) nullValueSatisfiesInputValueDefinition(inputValueDefinition int) bool {
	inputType := v.definition.Types[v.definition.InputValueDefinitionType(inputValueDefinition)]
	return inputType.TypeKind != ast.TypeKindNonNull
}

func (v *validArgumentsVisitor) enumValueSatisfiesInputValueDefinition(enumValue, inputValueDefinition int) bool {

	definitionTypeName := v.definition.ResolveTypeName(v.definition.InputValueDefinitions[inputValueDefinition].Type)
	node, exists := v.definition.Index.Nodes[string(definitionTypeName)]
	if !exists {
		return false
	}

	if node.Kind != ast.NodeKindEnumTypeDefinition {
		return false
	}

	enumValueName := v.operation.Input.ByteSlice(v.operation.EnumValueName(enumValue))

	if !v.definition.EnumTypeDefinitionContainsEnumValue(node.Ref, enumValueName) {
		return false
	}

	return true
}

func (v *validArgumentsVisitor) variableValueSatisfiesInputValueDefinition(variableValue, inputValueDefinition int) bool {
	variableName := v.operation.VariableValueNameBytes(variableValue)
	variableDefinition, exists := v.operation.VariableDefinitionByName(variableName)
	if !exists {
		return false
	}

	operationType := v.operation.VariableDefinitions[variableDefinition].Type
	definitionType := v.definition.InputValueDefinitions[inputValueDefinition].Type
	hasDefaultValue := v.operation.VariableDefinitions[variableDefinition].DefaultValue.IsDefined ||
		v.definition.InputValueDefinitions[inputValueDefinition].DefaultValue.IsDefined

	if !v.operationTypeSatisfiesDefinitionType(operationType, definitionType, hasDefaultValue) {
		return false
	}

	return true
}

func (v *validArgumentsVisitor) operationTypeSatisfiesDefinitionType(operationType int, definitionType int, hasDefaultValue bool) bool {

	if operationType == -1 || definitionType == -1 {
		return false
	}

	if v.operation.Types[operationType].TypeKind != ast.TypeKindNonNull &&
		v.definition.Types[definitionType].TypeKind == ast.TypeKindNonNull &&
		hasDefaultValue &&
		v.definition.Types[definitionType].OfType != -1 {
		definitionType = v.definition.Types[definitionType].OfType
	}

	if v.operation.Types[operationType].TypeKind == ast.TypeKindNonNull &&
		v.definition.Types[definitionType].TypeKind != ast.TypeKindNonNull &&
		v.operation.Types[operationType].OfType != -1 {
		operationType = v.operation.Types[operationType].OfType
	}

	for {
		if operationType == -1 || definitionType == -1 {
			return false
		}
		if v.operation.Types[operationType].TypeKind != v.definition.Types[definitionType].TypeKind {
			return false
		}
		if v.operation.Types[operationType].TypeKind == ast.TypeKindNamed {
			return bytes.Equal(v.operation.Input.ByteSlice(v.operation.Types[operationType].Name),
				v.definition.Input.ByteSlice(v.definition.Types[definitionType].Name))
		}
		operationType = v.operation.Types[operationType].OfType
		definitionType = v.definition.Types[definitionType].OfType
	}
}

func Values() Rule {
	return func(walker *astvisitor.Walker) {
		visitor := valuesVisitor{
			Walker: walker,
		}
		walker.RegisterEnterDocumentVisitor(&visitor)
		walker.RegisterEnterArgumentVisitor(&visitor)
	}
}

type valuesVisitor struct {
	*astvisitor.Walker
	operation, definition *ast.Document
}

func (v *valuesVisitor) EnterDocument(operation, definition *ast.Document) {
	v.operation = operation
	v.definition = definition
}

func (v *valuesVisitor) EnterArgument(ref int) {

	definition, exists := v.ArgumentInputValueDefinition(ref)

	if !exists {
		argName := v.operation.ArgumentNameBytes(ref)
		nodeName := v.operation.NodeNameBytes(v.Ancestors[len(v.Ancestors)-1])
		v.StopWithExternalErr(operationreport.ErrArgumentNotDefinedOnNode(argName, nodeName))
		return
	}

	value := v.operation.ArgumentValue(ref)
	if value.Kind == ast.ValueKindVariable {
		variableName := v.operation.VariableValueNameBytes(value.Ref)
		variableDefinition, exists := v.operation.VariableDefinitionByName(variableName)
		if !exists {
			operationName := v.operation.NodeNameBytes(v.Ancestors[0])
			v.StopWithExternalErr(operationreport.ErrVariableNotDefinedOnOperation(variableName, operationName))
			return
		}
		if !v.operation.VariableDefinitions[variableDefinition].DefaultValue.IsDefined {
			return // variable has no default value, deep type check not required
		}
		value = v.operation.VariableDefinitions[variableDefinition].DefaultValue.Value
	}

	if !v.valueSatisfiesInputValueDefinitionType(value, v.definition.InputValueDefinitions[definition].Type) {

		printedValue, err := v.operation.PrintValueBytes(value, nil)
		if v.HandleInternalErr(err) {
			return
		}

		printedType, err := v.definition.PrintTypeBytes(v.definition.InputValueDefinitions[definition].Type, nil)
		if v.HandleInternalErr(err) {
			return
		}

		v.StopWithExternalErr(operationreport.ErrValueDoesntSatisfyInputValueDefinition(printedValue, printedType))
		return
	}
}

func (v *valuesVisitor) valueSatisfiesInputValueDefinitionType(value ast.Value, definitionTypeRef int) bool {

	switch v.definition.Types[definitionTypeRef].TypeKind {
	case ast.TypeKindNonNull:
		if value.Kind == ast.ValueKindNull {
			return false
		}
		return v.valueSatisfiesInputValueDefinitionType(value, v.definition.Types[definitionTypeRef].OfType)
	case ast.TypeKindNamed:
		node, exists := v.definition.Index.Nodes[string(v.definition.ResolveTypeName(definitionTypeRef))]
		if !exists {
			return false
		}
		return v.valueSatisfiesTypeDefinitionNode(value, node)
	case ast.TypeKindList:
		return v.valueSatisfiesListType(value, v.definition.Types[definitionTypeRef].OfType)
	default:
		return false
	}
}

func (v *valuesVisitor) valueSatisfiesListType(value ast.Value, listType int) bool {
	if value.Kind != ast.ValueKindList {
		return false
	}

	if v.definition.Types[listType].TypeKind == ast.TypeKindNonNull {
		if len(v.operation.ListValues[value.Ref].Refs) == 0 {
			return false
		}
		listType = v.definition.Types[listType].OfType
	}

	for _, i := range v.operation.ListValues[value.Ref].Refs {
		listValue := v.operation.Value(i)
		if !v.valueSatisfiesInputValueDefinitionType(listValue, listType) {
			return false
		}
	}

	return true
}

func (v *valuesVisitor) valueSatisfiesTypeDefinitionNode(value ast.Value, node ast.Node) bool {
	switch node.Kind {
	case ast.NodeKindEnumTypeDefinition:
		return v.valueSatisfiesEnum(value, node)
	case ast.NodeKindScalarTypeDefinition:
		return v.valueSatisfiesScalar(value, node.Ref)
	case ast.NodeKindInputObjectTypeDefinition:
		return v.valueSatisfiesInputObjectTypeDefinition(value, node.Ref)
	default:
		return false
	}
}

func (v *valuesVisitor) valueSatisfiesEnum(value ast.Value, node ast.Node) bool {
	if value.Kind != ast.ValueKindEnum {
		return false
	}
	enumValue := v.operation.EnumValueNameBytes(value.Ref)
	return v.definition.EnumTypeDefinitionContainsEnumValue(node.Ref, enumValue)
}

func (v *valuesVisitor) valueSatisfiesInputObjectTypeDefinition(value ast.Value, inputObjectTypeDefinition int) bool {
	if value.Kind != ast.ValueKindObject {
		return false
	}

	for _, i := range v.definition.InputObjectTypeDefinitions[inputObjectTypeDefinition].InputFieldsDefinition.Refs {
		if !v.objectValueSatisfiesInputValueDefinition(value.Ref, i) {
			return false
		}
	}

	for _, i := range v.operation.ObjectValues[value.Ref].Refs {
		if !v.objectFieldDefined(i, inputObjectTypeDefinition) {
			objectFieldName := string(v.operation.ObjectFieldName(i))
			def := string(v.definition.Input.ByteSlice(v.definition.InputObjectTypeDefinitions[inputObjectTypeDefinition].Name))
			_, _ = objectFieldName, def
			return false
		}
	}

	if v.objectValueHasDuplicateFields(value.Ref) {
		return false
	}

	return true
}

func (v *valuesVisitor) objectValueHasDuplicateFields(objectValue int) bool {
	for i, j := range v.operation.ObjectValues[objectValue].Refs {
		for k, l := range v.operation.ObjectValues[objectValue].Refs {
			if i == k || i > k {
				continue
			}
			if bytes.Equal(v.operation.ObjectFieldName(j), v.operation.ObjectFieldName(l)) {
				return true
			}
		}
	}
	return false
}

func (v *valuesVisitor) objectFieldDefined(objectField, inputObjectTypeDefinition int) bool {
	name := v.operation.ObjectFieldName(objectField)
	for _, i := range v.definition.InputObjectTypeDefinitions[inputObjectTypeDefinition].InputFieldsDefinition.Refs {
		if bytes.Equal(name, v.definition.InputValueDefinitionNameBytes(i)) {
			return true
		}
	}
	return false
}

func (v *valuesVisitor) objectValueSatisfiesInputValueDefinition(objectValue, inputValueDefinition int) bool {

	name := v.definition.InputValueDefinitionNameBytes(inputValueDefinition)
	definitionType := v.definition.InputValueDefinitionType(inputValueDefinition)

	for _, i := range v.operation.ObjectValues[objectValue].Refs {
		if bytes.Equal(name, v.operation.ObjectFieldName(i)) {
			value := v.operation.ObjectFieldValue(i)
			return v.valueSatisfiesInputValueDefinitionType(value, definitionType)
		}
	}

	// argument is not present on object value, if arg is optional it's still ok, otherwise not satisfied
	return v.definition.InputValueDefinitionArgumentIsOptional(inputValueDefinition)
}

func (v *valuesVisitor) valueSatisfiesScalar(value ast.Value, scalar int) bool {
	scalarName := v.definition.ScalarTypeDefinitionNameBytes(scalar)
	switch value.Kind {
	case ast.ValueKindString:
		return bytes.Equal(scalarName, literal.STRING)
	case ast.ValueKindBoolean:
		return bytes.Equal(scalarName, literal.BOOLEAN)
	case ast.ValueKindInteger:
		return bytes.Equal(scalarName, literal.INT) || bytes.Equal(scalarName, literal.FLOAT)
	case ast.ValueKindFloat:
		return bytes.Equal(scalarName, literal.FLOAT)
	default:
		return false
	}
}

func ArgumentUniqueness() Rule {
	return func(walker *astvisitor.Walker) {
		visitor := argumentUniquenessVisitor{
			Walker: walker,
		}
		walker.RegisterEnterDocumentVisitor(&visitor)
		walker.RegisterEnterArgumentVisitor(&visitor)
	}
}

type argumentUniquenessVisitor struct {
	*astvisitor.Walker
	operation *ast.Document
}

func (a *argumentUniquenessVisitor) EnterDocument(operation, definition *ast.Document) {
	a.operation = operation
}

func (a *argumentUniquenessVisitor) EnterArgument(ref int) {

	argumentName := a.operation.ArgumentNameBytes(ref)
	argumentsAfter := a.operation.ArgumentsAfter(a.Ancestors[len(a.Ancestors)-1], ref)

	for _, i := range argumentsAfter {
		if bytes.Equal(argumentName, a.operation.ArgumentNameBytes(i)) {
			a.StopWithExternalErr(operationreport.ErrArgumentMustBeUnique(argumentName))
			return
		}
	}
}

func RequiredArguments() Rule {
	return func(walker *astvisitor.Walker) {
		visitor := requiredArgumentsVisitor{
			Walker: walker,
		}
		walker.RegisterEnterDocumentVisitor(&visitor)
		walker.RegisterEnterFieldVisitor(&visitor)
	}
}

type requiredArgumentsVisitor struct {
	*astvisitor.Walker
	operation, definition *ast.Document
}

func (r *requiredArgumentsVisitor) EnterDocument(operation, definition *ast.Document) {
	r.operation = operation
	r.definition = definition
}

func (r *requiredArgumentsVisitor) EnterField(ref int) {

	fieldName := r.operation.FieldNameBytes(ref)
	inputValueDefinitions := r.definition.NodeFieldDefinitionArgumentsDefinitions(r.EnclosingTypeDefinition, fieldName)

	for _, i := range inputValueDefinitions {
		if r.definition.InputValueDefinitionArgumentIsOptional(i) {
			continue
		}

		name := r.definition.InputValueDefinitionNameBytes(i)

		argument, exists := r.operation.FieldArgument(ref, name)
		if !exists {
			r.StopWithExternalErr(operationreport.ErrArgumentRequiredOnField(name, fieldName))
			return
		}

		if r.operation.ArgumentValue(argument).Kind == ast.ValueKindNull {
			r.StopWithExternalErr(operationreport.ErrArgumentOnFieldMustNotBeNull(name, fieldName))
			return
		}
	}
}

func Fragments() Rule {
	return func(walker *astvisitor.Walker) {
		visitor := fragmentsVisitor{
			Walker:                     walker,
			fragmentDefinitionsVisited: make([]ast.ByteSlice, 0, 8),
		}
		walker.RegisterEnterDocumentVisitor(&visitor)
		walker.RegisterLeaveDocumentVisitor(&visitor)
		walker.RegisterEnterFragmentDefinitionVisitor(&visitor)
		walker.RegisterEnterInlineFragmentVisitor(&visitor)
		walker.RegisterEnterFragmentSpreadVisitor(&visitor)
	}
}

type fragmentsVisitor struct {
	*astvisitor.Walker
	operation, definition      *ast.Document
	fragmentDefinitionsVisited []ast.ByteSlice
}

func (f *fragmentsVisitor) EnterFragmentSpread(ref int) {
	if f.Ancestors[0].Kind == ast.NodeKindOperationDefinition {
		spreadName := f.operation.FragmentSpreadNameBytes(ref)
		f.StopWithExternalErr(operationreport.ErrFragmentSpreadFormsCycle(spreadName))
	}
}

func (f *fragmentsVisitor) LeaveDocument(operation, definition *ast.Document) {
	for i := range f.fragmentDefinitionsVisited {
		if !f.operation.FragmentDefinitionIsUsed(f.fragmentDefinitionsVisited[i]) {
			fragmentName := f.fragmentDefinitionsVisited[i]
			f.StopWithExternalErr(operationreport.ErrFragmentDefinedButNotUsed(fragmentName))
			return
		}
	}
}

func (f *fragmentsVisitor) fragmentOnNodeIsAllowed(node ast.Node) bool {
	switch node.Kind {
	case ast.NodeKindObjectTypeDefinition, ast.NodeKindInterfaceTypeDefinition, ast.NodeKindUnionTypeDefinition:
		return true
	default:
		return false
	}
}

func (f *fragmentsVisitor) EnterInlineFragment(ref int) {

	if !f.operation.InlineFragmentHasTypeCondition(ref) {
		return
	}

	typeName := f.operation.InlineFragmentTypeConditionName(ref)

	node, exists := f.definition.Index.Nodes[string(typeName)]
	if !exists {
		f.StopWithExternalErr(operationreport.ErrTypeUndefined(typeName))
		return
	}

	if !f.fragmentOnNodeIsAllowed(node) {
		f.StopWithExternalErr(operationreport.ErrInlineFragmentOnTypeDisallowed(typeName))
		return
	}

	if !f.definition.NodeFragmentIsAllowedOnNode(node, f.EnclosingTypeDefinition) {
		enclosingTypeName := f.definition.NodeNameBytes(f.EnclosingTypeDefinition)
		f.StopWithExternalErr(operationreport.ErrInlineFragmentOnTypeMismatchEnclosingType(typeName, enclosingTypeName))
		return
	}
}

func (f *fragmentsVisitor) EnterDocument(operation, definition *ast.Document) {
	f.operation = operation
	f.definition = definition
	f.fragmentDefinitionsVisited = f.fragmentDefinitionsVisited[:0]
}

func (f *fragmentsVisitor) EnterFragmentDefinition(ref int) {

	fragmentDefinitionName := f.operation.FragmentDefinitionNameBytes(ref)
	typeName := f.operation.FragmentDefinitionTypeName(ref)

	node, exists := f.definition.Index.Nodes[string(typeName)]
	if !exists {
		f.StopWithExternalErr(operationreport.ErrTypeUndefined(typeName))
		return
	}

	if !f.fragmentOnNodeIsAllowed(node) {
		f.StopWithExternalErr(operationreport.ErrFragmentDefinitionOnTypeDisallowed(fragmentDefinitionName, typeName))
		return
	}

	for i := range f.fragmentDefinitionsVisited {
		if bytes.Equal(fragmentDefinitionName, f.fragmentDefinitionsVisited[i]) {
			f.StopWithExternalErr(operationreport.ErrFragmentDefinitionMustBeUnique(fragmentDefinitionName))
			return
		}
	}

	f.fragmentDefinitionsVisited = append(f.fragmentDefinitionsVisited, fragmentDefinitionName)
}

func DirectivesAreDefined() Rule {
	return func(walker *astvisitor.Walker) {
		visitor := directivesAreDefinedVisitor{
			Walker: walker,
		}
		walker.RegisterEnterDocumentVisitor(&visitor)
		walker.RegisterEnterDirectiveVisitor(&visitor)
	}
}

type directivesAreDefinedVisitor struct {
	*astvisitor.Walker
	operation, definition *ast.Document
}

func (d *directivesAreDefinedVisitor) EnterDocument(operation, definition *ast.Document) {
	d.operation = operation
	d.definition = definition
}

func (d *directivesAreDefinedVisitor) EnterDirective(ref int) {

	directiveName := d.operation.DirectiveNameBytes(ref)
	definition, exists := d.definition.Index.Nodes[string(directiveName)]

	if !exists || definition.Kind != ast.NodeKindDirectiveDefinition {
		d.StopWithExternalErr(operationreport.ErrDirectiveUndefined(directiveName))
		return
	}
}

func DirectivesAreInValidLocations() Rule {
	return func(walker *astvisitor.Walker) {
		visitor := directivesAreInValidLocationsVisitor{
			Walker: walker,
		}
		walker.RegisterEnterDocumentVisitor(&visitor)
		walker.RegisterEnterDirectiveVisitor(&visitor)
	}
}

type directivesAreInValidLocationsVisitor struct {
	*astvisitor.Walker
	operation, definition *ast.Document
}

func (d *directivesAreInValidLocationsVisitor) EnterDocument(operation, definition *ast.Document) {
	d.operation = operation
	d.definition = definition
}

func (d *directivesAreInValidLocationsVisitor) EnterDirective(ref int) {

	directiveName := d.operation.DirectiveNameBytes(ref)
	definition, exists := d.definition.Index.Nodes[string(directiveName)]

	if !exists || definition.Kind != ast.NodeKindDirectiveDefinition {
		return // not defined, skip
	}

	ancestor := d.Ancestors[len(d.Ancestors)-1]

	if !d.directiveDefinitionContainsNodeLocation(definition.Ref, ancestor) {
		ancestorKindName := d.operation.NodeKindNameBytes(ancestor)
		d.StopWithExternalErr(operationreport.ErrDirectiveNotAllowedOnNode(directiveName, ancestorKindName))
		return
	}
}

func (d *directivesAreInValidLocationsVisitor) directiveDefinitionContainsNodeLocation(definition int, node ast.Node) bool {

	nodeDirectiveLocation, err := d.operation.NodeDirectiveLocation(node)
	if err != nil {
		return false
	}

	return d.definition.DirectiveDefinitions[definition].DirectiveLocations.Get(nodeDirectiveLocation)
}

func VariableUniqueness() Rule {
	return func(walker *astvisitor.Walker) {
		visitor := variableUniquenessVisitor{
			Walker: walker,
		}
		walker.RegisterEnterDocumentVisitor(&visitor)
		walker.RegisterEnterVariableDefinitionVisitor(&visitor)
	}
}

type variableUniquenessVisitor struct {
	*astvisitor.Walker
	operation, definition *ast.Document
}

func (v *variableUniquenessVisitor) EnterDocument(operation, definition *ast.Document) {
	v.operation = operation
	v.definition = definition
}

func (v *variableUniquenessVisitor) EnterVariableDefinition(ref int) {

	name := v.operation.VariableDefinitionNameBytes(ref)

	if v.Ancestors[0].Kind != ast.NodeKindOperationDefinition {
		return
	}

	variableDefinitions := v.operation.OperationDefinitions[v.Ancestors[0].Ref].VariableDefinitions.Refs

	for _, i := range variableDefinitions {
		if i == ref {
			continue
		}
		if bytes.Equal(name, v.operation.VariableDefinitionNameBytes(i)) {
			if v.Ancestors[0].Kind != ast.NodeKindOperationDefinition {
				v.StopWithInternalErr(fmt.Errorf("variable definition must have Operation Definition as root ancestor, got: %s", v.Ancestors[0].Kind))
				return
			}
			operationName := v.operation.Input.ByteSlice(v.operation.OperationDefinitions[v.Ancestors[0].Ref].Name)
			v.StopWithExternalErr(operationreport.ErrVariableMustBeUnique(name, operationName))
			return
		}
	}
}

func DirectivesAreUniquePerLocation() Rule {
	return func(walker *astvisitor.Walker) {
		visitor := directivesAreUniquePerLocationVisitor{
			Walker: walker,
		}
		walker.RegisterEnterDocumentVisitor(&visitor)
		walker.RegisterEnterDirectiveVisitor(&visitor)
	}
}

type directivesAreUniquePerLocationVisitor struct {
	*astvisitor.Walker
	operation, definition *ast.Document
}

func (d *directivesAreUniquePerLocationVisitor) EnterDocument(operation, definition *ast.Document) {
	d.operation = operation
	d.definition = definition
}

func (d *directivesAreUniquePerLocationVisitor) EnterDirective(ref int) {

	directiveName := d.operation.DirectiveNameBytes(ref)
	directives := d.operation.NodeDirectives(d.Ancestors[len(d.Ancestors)-1])

	for _, j := range directives {
		if j == ref {
			continue
		}
		if bytes.Equal(directiveName, d.operation.DirectiveNameBytes(j)) {
			d.StopWithExternalErr(operationreport.ErrDirectiveMustBeUniquePerLocation(directiveName))
			return
		}
	}
}

func VariablesAreInputTypes() Rule {
	return func(walker *astvisitor.Walker) {
		visitor := variablesAreInputTypesVisitor{
			Walker: walker,
		}
		walker.RegisterEnterDocumentVisitor(&visitor)
		walker.RegisterEnterVariableDefinitionVisitor(&visitor)
	}
}

type variablesAreInputTypesVisitor struct {
	*astvisitor.Walker
	operation, definition *ast.Document
}

func (v *variablesAreInputTypesVisitor) EnterDocument(operation, definition *ast.Document) {
	v.operation = operation
	v.definition = definition
}

func (v *variablesAreInputTypesVisitor) EnterVariableDefinition(ref int) {

	typeName := v.operation.ResolveTypeName(v.operation.VariableDefinitions[ref].Type)
	typeDefinitionNode := v.definition.Index.Nodes[string(typeName)]
	switch typeDefinitionNode.Kind {
	case ast.NodeKindInputObjectTypeDefinition, ast.NodeKindScalarTypeDefinition, ast.NodeKindEnumTypeDefinition:
		return
	default:
		variableName := v.operation.VariableDefinitionNameBytes(ref)
		v.StopWithExternalErr(operationreport.ErrVariableOfTypeIsNoValidInputValue(variableName, typeName))
		return
	}
}

func AllVariableUsesDefined() Rule {
	return func(walker *astvisitor.Walker) {
		visitor := allVariableUsesDefinedVisitor{
			Walker: walker,
		}
		walker.RegisterEnterDocumentVisitor(&visitor)
		walker.RegisterEnterArgumentVisitor(&visitor)
	}
}

type allVariableUsesDefinedVisitor struct {
	*astvisitor.Walker
	operation, definition *ast.Document
}

func (a *allVariableUsesDefinedVisitor) EnterDocument(operation, definition *ast.Document) {
	a.operation = operation
	a.definition = definition
}

func (a *allVariableUsesDefinedVisitor) EnterArgument(ref int) {

	if a.operation.Arguments[ref].Value.Kind != ast.ValueKindVariable {
		return // skip because no variable
	}

	if a.Ancestors[0].Kind != ast.NodeKindOperationDefinition {
		// skip because variable is not used in operation which happens in case normalization did not merge the fragment definition
		// this happens when a fragment is defined but not used which will itself lead to another validation error
		// in which case we can safely skip here
		return
	}

	variableName := a.operation.VariableValueNameBytes(a.operation.Arguments[ref].Value.Ref)

	for _, i := range a.operation.OperationDefinitions[a.Ancestors[0].Ref].VariableDefinitions.Refs {
		if bytes.Equal(variableName, a.operation.VariableDefinitionNameBytes(i)) {
			return // return OK because variable is defined
		}
	}

	// at this point we're safe to say this variable was not defined on the root operation of this argument
	argumentName := a.operation.ArgumentNameBytes(ref)
	a.StopWithExternalErr(operationreport.ErrVariableNotDefinedOnArgument(variableName, argumentName))
}

func AllVariablesUsed() Rule {
	return func(walker *astvisitor.Walker) {
		visitor := allVariablesUsedVisitor{
			Walker: walker,
		}
		walker.RegisterEnterDocumentVisitor(&visitor)
		walker.RegisterEnterOperationVisitor(&visitor)
		walker.RegisterLeaveOperationVisitor(&visitor)
		walker.RegisterEnterArgumentVisitor(&visitor)
	}
}

type allVariablesUsedVisitor struct {
	*astvisitor.Walker
	operation, definition *ast.Document
	variableDefinitions   []int
}

func (a *allVariablesUsedVisitor) EnterDocument(operation, definition *ast.Document) {
	a.operation = operation
	a.definition = definition
}

func (a *allVariablesUsedVisitor) EnterOperationDefinition(ref int) {
	a.variableDefinitions = a.operation.OperationDefinitions[ref].VariableDefinitions.Refs
}

func (a *allVariablesUsedVisitor) LeaveOperationDefinition(ref int) {
	if len(a.variableDefinitions) != 0 {
		operationName := a.operation.Input.ByteSlice(a.operation.OperationDefinitions[ref].Name)
		for _, i := range a.variableDefinitions {
			variableName := a.operation.VariableDefinitionNameBytes(i)
			a.Report.AddExternalError(operationreport.ErrVariableDefinedButNeverUsed(variableName, operationName))
		}
		a.Stop()
	}
}

func (a *allVariablesUsedVisitor) EnterArgument(ref int) {

	if len(a.variableDefinitions) == 0 {
		return // nothing to check, skip
	}

	if a.operation.Arguments[ref].Value.Kind != ast.ValueKindVariable {
		return // skip non variable value
	}

	variableName := a.operation.VariableValueNameBytes(a.operation.Arguments[ref].Value.Ref)
	for i, j := range a.variableDefinitions {
		if bytes.Equal(variableName, a.operation.VariableDefinitionNameBytes(j)) {
			a.variableDefinitions = append(a.variableDefinitions[:i], a.variableDefinitions[i+1:]...)
			return
		}
	}
}
