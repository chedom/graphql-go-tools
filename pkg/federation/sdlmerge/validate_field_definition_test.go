package sdlmerge

import (
	"testing"
)

func TestValidateFieldDefinition(t *testing.T) {
	t.Run("No validation errors in input`s fields", func(t *testing.T) {
		run(t, newValidateFieldVisitor(), `
			input Trainer {
				name: String!
				age: Int!
			}

			input Trainer {
				name: String!
				age: Int!
			}
		`, `
			input Trainer {
				name: String!
				age: Int!
			}

			input Trainer {
				name: String!
				age: Int!
			}
		`)
	})

	t.Run("Same name inputs with different nullability of fields return an error", func(t *testing.T) {
		runAndExpectError(t, newValidateFieldVisitor(), `
			input Trainer {
				name: String!
				age: Int!
			}

			input Trainer {
				name: String
				age: Int!
			}
		`, NonIdenticalSharedTypeErrorMessage("Trainer"))
	})

	t.Run("Same name inputs with different types of fields return an error", func(t *testing.T) {
		runAndExpectError(t, newValidateFieldVisitor(), `
			input Trainer {
				name: String!
				age: Int!
			}

			input Trainer {
				name: Int!
				age: Int!
			}
		`, NonIdenticalSharedTypeErrorMessage("Trainer"))
	})

	t.Run("Same name inputs with a slight difference in nested field values return an error", func(t *testing.T) {
		runAndExpectError(t, newRemoveDuplicateFieldedSharedTypesVisitor(), `
			input Pokemon {
				type: [[[[Type!]]!]!]!
			}
	
			input Pokemon {
				type: [[[[Type!]]]!]!
			}
	
			input Pokemon {
				type: [[[[Type!]]!]!]!
			}
		`, NonIdenticalSharedTypeErrorMessage("Pokemon"))
	})

	t.Run("No validation errors in interface`s fields", func(t *testing.T) {
		run(t, newValidateFieldVisitor(), `
			interface Trainer {
				name: String!
				age: Int!
			}
			
			interface Trainer {
				name: String!
				age: Int!
			}
		`, `
			interface Trainer {
				name: String!
				age: Int!
			}
			interface Trainer {
				name: String!
				age: Int!
			}
		`)
	})

	t.Run("Same name interfaces with different nullability of fields return an error", func(t *testing.T) {
		runAndExpectError(t, newValidateFieldVisitor(), `
			interface Trainer {
				name: String!
				age: Int!
			}

			interface Trainer {
				name: String
				age: Int!
			}
		`, NonIdenticalSharedTypeErrorMessage("Trainer"))
	})

	t.Run("Same name interfaces with different types of fields return an error", func(t *testing.T) {
		runAndExpectError(t, newValidateFieldVisitor(), `
			interface Trainer {
				name: String!
				age: Int!
			}

			interface Trainer {
				name: Int!
				age: Int!
			}
		`, NonIdenticalSharedTypeErrorMessage("Trainer"))
	})

	t.Run("Same name interfaces with different number of input values (0 & 1) return an error", func(t *testing.T) {
		runAndExpectError(t, newValidateFieldVisitor(), `
			interface Trainer {
				name (id: String): String!
				age: Int!
			}

			interface Trainer {
				name: Int!
				age: Int!
			}
		`, NonIdenticalSharedTypeErrorMessage("Trainer"))
	})

	t.Run("Same name interfaces with different number of input values (2 & 1) return an error", func(t *testing.T) {
		runAndExpectError(t, newValidateFieldVisitor(), `
			interface Trainer {
				name (id: String, name: Int): String!
				age: Int!
			}

			interface Trainer {
				name (id: String): Int!
				age: Int!
			}
		`, NonIdenticalSharedTypeErrorMessage("Trainer"))
	})

	t.Run("Same name interfaces with different names of input values return an error", func(t *testing.T) {
		runAndExpectError(t, newValidateFieldVisitor(), `
			interface Trainer {
				name (id: Int): String!
				age: Int!
			}

			interface Trainer {
				name (badges: Int): Int!
				age: Int!
			}
		`, NonIdenticalSharedTypeErrorMessage("Trainer"))
	})

	t.Run("Same name interfaces with different nullability of field`s input values return an error", func(t *testing.T) {
		runAndExpectError(t, newValidateFieldVisitor(), `
			interface Trainer {
				name (id: Int): String!
				age: Int!
			}

			interface Trainer {
				name (id: Int!): Int!
				age: Int!
			}
		`, NonIdenticalSharedTypeErrorMessage("Trainer"))
	})

	t.Run("Same name interfaces with different types of field`s input values return an error", func(t *testing.T) {
		runAndExpectError(t, newValidateFieldVisitor(), `
			interface Trainer {
				name (id: Int!): String!
				age: Int!
			}

			interface Trainer {
				name (id: String!): Int!
				age: Int!
			}
		`, NonIdenticalSharedTypeErrorMessage("Trainer"))
	})

	t.Run("Same name interfaces with a slight difference in nested field values return an error", func(t *testing.T) {
		runAndExpectError(t, newValidateFieldVisitor(), `
			type Pokemon {
				type: [[[[Type!]]!]!]!
			}
	
			type Pokemon {
				type: [[[[Type!]]]!]!
			}
	
			type Pokemon {
				type: [[[[Type!]]!]!]!
			}
		`, NonIdenticalSharedTypeErrorMessage("Pokemon"))
	})

	t.Run("No validation errors in object`s fields", func(t *testing.T) {
		run(t, newValidateFieldVisitor(), `
			type Trainer {
				name: String!
				age: Int!
			}

			type Trainer {
				name: String!
				age: Int!
			}
		`, `
			type Trainer {
				name: String!
				age: Int!
			}

			type Trainer {
				name: String!
				age: Int!
			}
		`)
	})

	t.Run("Same name objects with different nullability of fields return an error", func(t *testing.T) {
		runAndExpectError(t, newValidateFieldVisitor(), `
			type Trainer {
				name: String!
				age: Int!
			}

			type Trainer {
				name: String
				age: Int!
			}
		`, NonIdenticalSharedTypeErrorMessage("Trainer"))
	})

	t.Run("Same name objects with different types of fields return an error", func(t *testing.T) {
		runAndExpectError(t, newValidateFieldVisitor(), `
			type Trainer {
				name: String!
				age: Int!
			}

			type Trainer {
				name: Int!
				age: Int!
			}
		`, NonIdenticalSharedTypeErrorMessage("Trainer"))
	})

	t.Run("Same name objects with different number of input values (0 & 1) return an error", func(t *testing.T) {
		runAndExpectError(t, newValidateFieldVisitor(), `
			type Trainer {
				name (id: String): String!
				age: Int!
			}

			type Trainer {
				name: Int!
				age: Int!
			}
		`, NonIdenticalSharedTypeErrorMessage("Trainer"))
	})

	t.Run("Same name objects with different number of input values (2 & 1) return an error", func(t *testing.T) {
		runAndExpectError(t, newValidateFieldVisitor(), `
			type Trainer {
				name (id: String, name: Int): String!
				age: Int!
			}

			type Trainer {
				name (id: String): Int!
				age: Int!
			}
		`, NonIdenticalSharedTypeErrorMessage("Trainer"))
	})

	t.Run("Same name objects with different names of input values return an error", func(t *testing.T) {
		runAndExpectError(t, newValidateFieldVisitor(), `
			type Trainer {
				name (id: Int): String!
				age: Int!
			}

			type Trainer {
				name (badges: Int): Int!
				age: Int!
			}
		`, NonIdenticalSharedTypeErrorMessage("Trainer"))
	})

	t.Run("Same name objects with different nullability of field`s input values return an error", func(t *testing.T) {
		runAndExpectError(t, newValidateFieldVisitor(), `
			type Trainer {
				name (id: Int): String!
				age: Int!
			}

			type Trainer {
				name (id: Int!): Int!
				age: Int!
			}
		`, NonIdenticalSharedTypeErrorMessage("Trainer"))
	})

	t.Run("Same name objects with different types of field`s input values return an error", func(t *testing.T) {
		runAndExpectError(t, newValidateFieldVisitor(), `
			type Trainer {
				name (id: Int!): String!
				age: Int!
			}

			type Trainer {
				name (id: String!): Int!
				age: Int!
			}
		`, NonIdenticalSharedTypeErrorMessage("Trainer"))
	})

	t.Run("Same name objects with a slight difference in nested field values return an error", func(t *testing.T) {
		runAndExpectError(t, newValidateFieldVisitor(), `
			type Pokemon {
				type: [[[[Type!]]!]!]!
			}
	
			type Pokemon {
				type: [[[[Type!]]]!]!
			}
	
			type Pokemon {
				type: [[[[Type!]]!]!]!
			}
		`, NonIdenticalSharedTypeErrorMessage("Pokemon"))
	})
}
