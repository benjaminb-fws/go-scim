package json

import (
	"bytes"
	"github.com/imulab/go-scim/src/core"
	"github.com/imulab/go-scim/src/core/errors"
	"github.com/imulab/go-scim/src/core/expr"
	"github.com/imulab/go-scim/src/core/prop"
	"math"
	"strconv"
	"unicode/utf8"
)

const (
	containerObject container = iota
	containerArray
)

type (
	// type of the containing property
	container int
	// stack frame during the traversal
	frame struct {
		// the type of the containing property
		container container
		// index of the element within the container
		index int
	}
	// json serializer state
	serializer struct {
		bytes.Buffer
		includeFamily *expr.PathAncestry
		excludeFamily *expr.PathAncestry
		stack         []*frame
		scratch       [64]byte
	}
)

// Serialize the given resource to JSON bytes.
func Serialize(resource *prop.Resource, options *options) ([]byte, error) {
	if options == nil {
		options = Options()
	}

	if len(options.included) > 0 && len(options.excluded) > 0 {
		return nil, errors.InvalidRequest("only one of 'attributes' and 'excludedAttributes' may be used")
	}

	s := new(serializer)
	if len(options.included) > 0 {
		s.includeFamily = expr.NewPathFamily(resource.ResourceType())
		for _, path := range options.included {
			if p, err := expr.CompilePath(path); err != nil {
				return nil, err
			} else {
				s.includeFamily.Add(p)
			}
		}
	} else if len(options.excluded) > 0 {
		s.excludeFamily = expr.NewPathFamily(resource.ResourceType())
		for _, path := range options.excluded {
			if p, err := expr.CompilePath(path); err != nil {
				return nil, err
			} else {
				s.excludeFamily.Add(p)
			}
		}
	}

	err := resource.Visit(s)
	if err != nil {
		return nil, errors.Internal("JSON serialization error: %s", err.Error())
	}

	return s.Bytes(), nil
}

func (s *serializer) ShouldVisit(property core.Property) bool {
	attr := property.Attribute()

	// Write only properties are never returned. It is usually coupled
	// with returned=never, but we will check it to make sure.
	if attr.Mutability() == core.MutabilityWriteOnly {
		return false
	}

	switch attr.Returned() {
	case core.ReturnedAlways:
		return true
	case core.ReturnedNever:
		return false
	case core.ReturnedDefault:
		if s.includeFamily == nil && s.excludeFamily == nil {
			return !property.IsUnassigned()
		} else {
			// All attribute IDs should have been pre-compiled and cached.
			p := expr.MustPath(property.Attribute().ID())
			if s.includeFamily != nil {
				return s.includeFamily.IsMember(p) || s.includeFamily.IsAncestor(p) || s.includeFamily.IsOffspring(p)
			} else if s.excludeFamily != nil {
				return s.excludeFamily.IsMember(p) || s.excludeFamily.IsOffspring(p)
			} else {
				panic("impossible: either includeFamily or excludeFamily")
			}
		}
	case core.ReturnedRequest:
		if s.includeFamily != nil {
			p, _ := expr.CompilePath(property.Attribute().ID())
			return s.includeFamily.IsMember(p) || s.includeFamily.IsAncestor(p) || s.includeFamily.IsOffspring(p)
		}
		return false
	default:
		panic("invalid returned-ability")
	}
}

func (s *serializer) Visit(property core.Property) (err error) {
	if s.current().index > 0 {
		_ = s.WriteByte(',')
	}

	if s.current().container != containerArray {
		s.appendPropertyName(property.Attribute())
	}

	if _, ok := property.(core.Container); ok {
		return
	}

	if property.IsUnassigned() {
		s.appendNull()
		return nil
	}

	switch property.Attribute().Type() {
	case core.TypeString, core.TypeReference, core.TypeDateTime, core.TypeBinary:
		s.appendString(property.Raw().(string))
	case core.TypeInteger:
		s.appendInteger(property.Raw().(int64))
	case core.TypeDecimal:
		s.appendFloat(property.Raw().(float64))
	case core.TypeBoolean:
		s.appendBoolean(property.Raw().(bool))
	default:
		panic("invalid type")
	}

	s.current().index++
	return
}

func (s *serializer) BeginChildren(container core.Container) {
	switch {
	case container.Attribute().MultiValued():
		_ = s.WriteByte('[')
		s.push(containerArray)
	case container.Attribute().Type() == core.TypeComplex:
		_ = s.WriteByte('{')
		s.push(containerObject)
	default:
		panic("unknown container")
	}
}

func (s *serializer) EndChildren(container core.Container) {
	switch {
	case container.Attribute().MultiValued():
		_ = s.WriteByte(']')
	case container.Attribute().Type() == core.TypeComplex:
		_ = s.WriteByte('}')
	default:
		panic("unknown container")
	}
	s.pop()
	if len(s.stack) > 0 {
		s.current().index++
	}

}

func (s *serializer) appendPropertyName(attribute *core.Attribute) {
	_ = s.WriteByte('"')
	_, _ = s.WriteString(attribute.Name())
	_ = s.WriteByte('"')
	_ = s.WriteByte(':')
}

func (s *serializer) appendNull() {
	_, _ = s.WriteString("null")
}

func (s *serializer) appendString(value string) {
	_ = s.WriteByte('"')
	start := 0
	for i := 0; i < len(value); {
		if b := value[i]; b < utf8.RuneSelf {
			if htmlSafeSet[b] {
				i++
				continue
			}
			if start < i {
				_, _ = s.WriteString(value[start:i])
			}
			_ = s.WriteByte('\\')
			switch b {
			case '\\', '"':
				_ = s.WriteByte(b)
			case '\n':
				_ = s.WriteByte('n')
			case '\r':
				_ = s.WriteByte('r')
			case '\t':
				_ = s.WriteByte('t')
			default:
				// This encodes bytes < 0x20 except for \t, \n and \r.
				// If escapeHTML is set, it also escapes <, >, and &
				// because they can lead to security holes when
				// user-controlled strings are rendered into JSON
				// and served to some browsers.
				_, _ = s.WriteString(`u00`)
				_ = s.WriteByte(hex[b>>4])
				_ = s.WriteByte(hex[b&0xF])
			}
			i++
			start = i
			continue
		}
		c, size := utf8.DecodeRuneInString(value[i:])
		if c == utf8.RuneError && size == 1 {
			if start < i {
				_, _ = s.WriteString(value[start:i])
			}
			_, _ = s.WriteString(`\ufffd`)
			i += size
			start = i
			continue
		}
		// U+2028 is LINE SEPARATOR.
		// U+2029 is PARAGRAPH SEPARATOR.
		// They are both technically valid characters in JSON strings,
		// but don't work in JSONP, which has to be evaluated as JavaScript,
		// and can lead to security holes there. It is valid JSON to
		// escape them, so we do so unconditionally.
		// See http://timelessrepo.com/json-isnt-a-javascript-subset for discussion.
		if c == '\u2028' || c == '\u2029' {
			if start < i {
				_, _ = s.WriteString(value[start:i])
			}
			_, _ = s.WriteString(`\u202`)
			_ = s.WriteByte(hex[c&0xF])
			i += size
			start = i
			continue
		}
		i += size
	}
	if start < len(value) {
		_, _ = s.WriteString(value[start:])
	}
	_ = s.WriteByte('"')
}

func (s *serializer) appendInteger(value int64) {
	b := strconv.AppendInt(s.scratch[:0], value, 10)
	_, _ = s.Write(b)
}

func (s *serializer) appendFloat(value float64) {
	if math.IsInf(value, 0) || math.IsNaN(value) {
		panic(errors.Internal("%f is not a valid decimal", value))
	}

	// Convert as if by ES6 number to string conversion.
	// This matches most other JSON generators.
	// See golang.org/issue/6384 and golang.org/issue/14135.
	// Like fmt %g, but the exponent cutoffs are different
	// and exponents themselves are not padded to two digits.
	b := s.scratch[:0]
	abs := math.Abs(value)
	format := byte('f')
	if abs != 0 {
		if abs < 1e-6 || abs >= 1e21 {
			format = 'e'
		}
	}
	b = strconv.AppendFloat(b, value, format, -1, 64)
	if format == 'e' {
		// clean up e-09 to e-9
		n := len(b)
		if n >= 4 && b[n-4] == 'e' && b[n-3] == '-' && b[n-2] == '0' {
			b[n-2] = b[n-1]
			b = b[:n-1]
		}
	}
	_, _ = s.Write(b)
}

func (s *serializer) appendBoolean(value bool) {
	if value {
		_, _ = s.WriteString("true")
	} else {
		_, _ = s.WriteString("false")
	}
}

func (s *serializer) push(c container) {
	s.stack = append(s.stack, &frame{
		container: c,
		index:     0,
	})
}

func (s *serializer) pop() {
	if len(s.stack) == 0 {
		panic("cannot pop on empty stack")
	}
	s.stack = s.stack[:len(s.stack)-1]
}

func (s *serializer) current() *frame {
	if len(s.stack) == 0 {
		panic("stack is empty")
	}
	return s.stack[len(s.stack)-1]
}
