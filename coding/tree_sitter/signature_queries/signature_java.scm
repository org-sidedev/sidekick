;;----------------------------------------------------------------------------
;; Unified Top-Level Queries for Declarations and Members
;;----------------------------------------------------------------------------
;; These queries capture non-private/protected top-level declarations
;; (classes, interfaces, enums, annotations) and their non-private/protected
;; members (methods, fields, constructors, enum constants, annotation elements).
;; Member queries include predicates to check both member and container visibility.
;; Capture names are designed for compatibility with writeJavaSignatureCapture (hierarchical)
;; and symbol extraction (simple @*.name captures).
;;----------------------------------------------------------------------------

;; Class Declaration (captures the class itself)
(class_declaration
  (modifiers)? @class.modifiers
    (#not-match? @class.modifiers "(private|protected)")
  name: (identifier) @class.name
  (type_parameters)? @class.type_parameters
) @class.declaration

;; Interface Declaration (captures the interface itself)
(interface_declaration
  (modifiers)? @interface.modifiers
    (#not-match? @interface.modifiers "(private|protected)")
  name: (identifier) @interface.name
  (type_parameters)? @interface.type_parameters
) @interface.declaration

;; Enum Declaration (captures the enum itself)
(enum_declaration
  (modifiers)? @enum.modifiers
    (#not-match? @enum.modifiers "(private|protected)")
  name: (identifier) @enum.name
) @enum.declaration

;; Annotation Type Declaration (captures the annotation type itself)
(annotation_type_declaration
  (modifiers)? @annotation.modifiers
    (#not-match? @annotation.modifiers "(private|protected)")
  name: (identifier) @annotation.name
) @annotation.declaration

;; Methods in Classes
(class_declaration
  (modifiers)? @method.class.modifiers ; For parent visibility predicate check
  (class_body
    (method_declaration
      (modifiers)? @method.modifiers
        (#not-match? @method.modifiers "(private|protected)")
      (type_parameters)? @method.type_parameters
      type: (_) @method.type
      name: (identifier) @method.name ; Hierarchical and simple name
      parameters: (_) @method.parameters
    ) @method.declaration ; Capture the whole method for signature context
  )
  (#not-match? @method.class.modifiers "(private|protected)")
)

;; Methods in Interfaces (default or static - abstract methods are part of interface decl)
(interface_declaration
  (modifiers)? @method.interface.modifiers ; For parent visibility predicate check
  (interface_body
    (method_declaration
      (modifiers)? @method.modifiers
        ; Interface methods are implicitly public unless private (Java 9+) or static/default
        (#not-match? @method.modifiers "(private)") ; Allow public (implicit), static, default
      (type_parameters)? @method.type_parameters
      type: (_) @method.type
      name: (identifier) @method.name
      parameters: (_) @method.parameters
    ) @method.declaration
  )
  (#not-match? @method.interface.modifiers "(private|protected)")
)

;; Methods in Enums
(enum_declaration
  (modifiers)? @method.enum.modifiers ; For parent visibility predicate check
  (enum_body
    (enum_body_declarations
      (method_declaration
        (modifiers)? @method.modifiers
        (type_parameters)? @method.type_parameters
        type: (_) @method.type
        name: (identifier) @method.name
        parameters: (_) @method.parameters
        (#not-match? @method.modifiers "(private|protected)")
      ) @method.declaration
    )
  )
  (#not-match? @method.enum.modifiers "(private|protected)")
)

;; Constructors in Classes
(class_declaration
  (modifiers)? @constructor.class.modifiers ; For parent visibility predicate check
  (class_body
    (constructor_declaration
      (modifiers)? @constructor.modifiers
      name: (identifier) @constructor.name
      parameters: (_) @constructor.parameters
      (#not-match? @constructor.modifiers "(private|protected)")
    ) @constructor.declaration ; Capture the whole constructor
  )
  (#not-match? @constructor.class.modifiers "(private|protected)")
)

;; fields/constants in Classes (captures each declarator)
(class_declaration
  (modifiers)? @field.class.modifiers ; For parent visibility predicate check
  (class_body
    (field_declaration
      (modifiers)? @field.modifiers
      type: (_) @field.type
      (variable_declarator
        name: (identifier) @field.name
        value: (_)? @field.value
        dimensions: (_)? @field.dimensions
      ) @declarator ; Capture each declarator
      (#not-match? @field.modifiers "(private|protected)")
    ) @field.declaration ; Capture the whole field declaration (match per declarator)
  )
  (#not-match? @field.class.modifiers "(private|protected)")
)

;; Constants in Interfaces (captures each declarator)
(interface_declaration
  (modifiers)? @constant.interface.modifiers ; For parent visibility predicate check
  (interface_body
    (constant_declaration ; Constants in interfaces are implicitly public static final
      (modifiers)? @constant.modifiers
      type: (_) @constant.type
      (variable_declarator
        name: (identifier) @constant.name
        value: (_) @constant.value
        dimensions: (_)? @constant.dimensions
      ) @declarator
      ; No visibility check needed, implicitly public
    ) @constant.declaration
  )
  (#not-match? @constant.interface.modifiers "(private|protected)")
)

;; Enum Constants
(enum_declaration
  (modifiers)? @constant.enum.modifiers ; For parent visibility predicate check
  (enum_body
    (enum_constant
      (modifiers)? @constant.modifiers
      name: (identifier) @constant.name ; Hierarchical and simple name
      arguments: (_)? @constant.arguments
    ) @constant.declaration ; Capture the constant
    ; No explicit visibility check needed for enum constants, they are implicitly public static final
  )
  (#not-match? @constant.enum.modifiers "(private|protected)")
)

;; Annotation Type Constants
(annotation_type_declaration
  (modifiers)? @constant.annotation.modifiers ; For parent visibility predicate check
  (annotation_type_body
    (constant_declaration
      (modifiers)? @constant.modifiers
      type: (_) @constant.type
      (variable_declarator
        name: (identifier) @constant.name
        value: (_) @constant.value
        dimensions: (_)? @constant.dimensions
      ) @declarator
    ) @constant.declaration
  )
  (#not-match? @constant.annotation.modifiers "(private|protected)")
)

;; Annotation Type Elements
(annotation_type_declaration
  (modifiers)? @annotation_type_element.annotation.modifiers ; For parent visibility predicate check
  (annotation_type_body
    (annotation_type_element_declaration
      (modifiers)? @annotation_type_element.modifiers
        (#not-match? @annotation_type_element.modifiers "(private|protected)")
      type: (_) @annotation_type_element.type
      name: (identifier) @annotation_type_element.name
      ; No explicit visibility check needed, implicitly public abstract
    ) @annotation_type_element.declaration ; Capture the element
  )
  (#not-match? @annotation_type_element.annotation.modifiers "(private|protected)")
)