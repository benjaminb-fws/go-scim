package prop

import (
	"encoding/binary"
	"github.com/imulab/go-scim/pkg/core"
	"github.com/imulab/go-scim/pkg/core/annotations"
	"github.com/imulab/go-scim/pkg/core/errors"
	"hash/fnv"
)

// Create a new unassigned multiValued property. The method will panic if
// given attribute is not multiValued type.
func NewMulti(attr *core.Attribute, parent core.Container) core.Property {
	if !attr.MultiValued() {
		panic("invalid attribute for multiValued property")
	}
	p := &multiValuedProperty{
		parent:      parent,
		attr:        attr,
		elements:    make([]core.Property, 0),
		subscribers: []core.Subscriber{},
	}
	subscribeWithAnnotation(p)
	return p
}

// Create a new multiValued property with given value. The method will panic if
// given attribute is not multiValued type. The property will be
// marked dirty at the start.
func NewMultiOf(attr *core.Attribute, parent core.Container, value interface{}) core.Property {
	p := NewMulti(attr, parent)
	if err := p.Add(value); err != nil {
		panic(err)
	}
	return p
}

var (
	_ core.Property  = (*multiValuedProperty)(nil)
	_ core.Container = (*multiValuedProperty)(nil)
)

type multiValuedProperty struct {
	parent      core.Container
	attr        *core.Attribute
	elements    []core.Property
	touched     bool
	subscribers []core.Subscriber
}

func (p *multiValuedProperty) Clone(parent core.Container) core.Property {
	c := &multiValuedProperty{
		parent:      parent,
		attr:        p.attr,
		elements:    make([]core.Property, 0),
		touched:     p.touched,
		subscribers: p.subscribers,
	}
	for _, elem := range p.elements {
		c.elements = append(c.elements, elem.Clone(parent))
	}
	return c
}

func (p *multiValuedProperty) Parent() core.Container {
	return p.parent
}

func (p *multiValuedProperty) Subscribe(subscriber core.Subscriber) {
	p.subscribers = append(p.subscribers, subscriber)
}

func (p *multiValuedProperty) Propagate(e *core.Event) error {
	if len(p.subscribers) > 0 {
		for _, subscriber := range p.subscribers {
			if err := subscriber.Notify(p, e); err != nil {
				return err
			}
		}
	}
	if p.parent != nil && e.WillPropagate() {
		if err := p.parent.Propagate(e); err != nil {
			return err
		}
	}
	return nil
}

func (p *multiValuedProperty) Attribute() *core.Attribute {
	return p.attr
}

func (p *multiValuedProperty) Raw() interface{} {
	if len(p.elements) == 0 {
		return nil
	}
	values := make([]interface{}, len(p.elements), len(p.elements))
	for i, elem := range p.elements {
		values[i] = elem.Raw()
	}
	return values
}

func (p *multiValuedProperty) IsUnassigned() bool {
	return len(p.elements) == 0
}

func (p *multiValuedProperty) ModCount() int {
	return 0 // multiValued property is just a container
}

func (p *multiValuedProperty) Matches(another core.Property) bool {
	if !p.Attribute().Equals(another.Attribute()) {
		return false
	}

	if len(p.elements) == 0 {
		return len(another.(*multiValuedProperty).elements) == 0
	}

	return p.Hash() == another.Hash()
}

func (p *multiValuedProperty) Hash() uint64 {
	if len(p.elements) == 0 {
		return 0
	}

	hashes := make([]uint64, 0)
	_ = p.ForEachChild(func(index int, child core.Property) error {
		if child.IsUnassigned() {
			return nil
		}

		// SCIM array does not have orders. We keep the hash array
		// sorted so that different multiValue properties containing
		// the same elements in different orders can be recognized as
		// the same, as they compute the same hash. We use insertion
		// sort here as we don't expect a large number of elements.
		hashes = append(hashes, child.Hash())
		for i := len(hashes) - 1; i > 0; i-- {
			if hashes[i-1] > hashes[i] {
				hashes[i-1], hashes[i] = hashes[i], hashes[i-1]
			}
		}
		return nil
	})

	h := fnv.New64a()
	for _, hash := range hashes {
		b := make([]byte, 8)
		binary.LittleEndian.PutUint64(b, hash)
		_, err := h.Write(b)
		if err != nil {
			panic("error computing hash")
		}
	}

	return h.Sum64()
}

func (p *multiValuedProperty) EqualsTo(value interface{}) (bool, error) {
	// This method is counter intuitive. It is implemented to allow for the
	// special scenario where SCIM uses 'eq' operator to match an element
	// within a multiValued property. Hence, consider this a special contains
	// operation.
	for _, elem := range p.elements {
		equal, err := elem.EqualsTo(value)
		if equal && err == nil {
			return true, nil
		}
	}
	return false, nil
}

func (p *multiValuedProperty) StartsWith(value string) (bool, error) {
	return false, p.errIncompatibleOp()
}

func (p *multiValuedProperty) EndsWith(value string) (bool, error) {
	return false, p.errIncompatibleOp()
}

func (p *multiValuedProperty) Contains(value string) (bool, error) {
	return false, p.errIncompatibleOp()
}

func (p *multiValuedProperty) GreaterThan(value interface{}) (bool, error) {
	return false, p.errIncompatibleOp()
}

func (p *multiValuedProperty) LessThan(value interface{}) (bool, error) {
	return false, p.errIncompatibleOp()
}

func (p *multiValuedProperty) Present() bool {
	return len(p.elements) > 0
}

func (p *multiValuedProperty) Add(value interface{}) error {
	if value == nil {
		return nil
	}

	// transform value into properties to add
	var (
		toAdd = make([]core.Property, 0)
		p0    core.Property
		err   error
	)
	{
		switch val := value.(type) {
		case []interface{}:
			for _, v := range val {
				if v == nil {
					continue
				}
				p0, err = p.newElementProperty(v)
				if err != nil {
					return err
				}
				toAdd = append(toAdd, p0)
			}
		default:
			p0, err = p.newElementProperty(val)
			if err != nil {
				return err
			}
			toAdd = append(toAdd, p0)
		}
	}

	if len(toAdd) == 0 {
		return nil
	}

	// Add each candidate only if they do not match existing elements
	for _, eachToAdd := range toAdd {
		match := false
		for _, elem := range p.elements {
			if elem.Matches(eachToAdd) {
				match = true
				break
			}
		}
		if !match {
			p.elements = append(p.elements, eachToAdd)
			p.touched = true
		}
	}

	return nil
}

func (p *multiValuedProperty) Replace(value interface{}) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = p.errIncompatibleValue(value)
		}
	}()

	err = p.Delete()
	if err != nil {
		return
	}

	err = p.Add(value)
	if err != nil {
		return
	}

	return
}

func (p *multiValuedProperty) Delete() error {
	p.elements = make([]core.Property, 0)
	p.touched = true
	return nil
}

func (p *multiValuedProperty) Touched() bool {
	return p.touched
}

func (p *multiValuedProperty) NewChild() int {
	c, err := p.newElementProperty(nil)
	if err != nil {
		return -1
	}
	p.elements = append(p.elements, c)
	return len(p.elements) - 1
}

func (p *multiValuedProperty) CountChildren() int {
	return len(p.elements)
}

func (p *multiValuedProperty) ChildAtIndex(index interface{}) core.Property {
	i, ok := index.(int)
	if !ok {
		return nil
	}

	if i >= len(p.elements) {
		return nil
	}

	return p.elements[i]
}

func (p *multiValuedProperty) ForEachChild(callback func(index int, child core.Property) error) error {
	for i, elem := range p.elements {
		if err := callback(i, elem); err != nil {
			return err
		}
	}
	return nil
}

func (p *multiValuedProperty) Compact() {
	if len(p.elements) == 0 {
		return
	}

	var i int
	for i = len(p.elements) - 1; i >= 0; i-- {
		if p.elements[i].IsUnassigned() {
			if i == len(p.elements)-1 {
				p.elements = p.elements[:i]
			} else if i == 0 {
				p.elements = p.elements[i+1:]
			} else {
				p.elements = append(p.elements[:i], p.elements[i+1:]...)
			}
		}
	}
}

func (p *multiValuedProperty) newElementProperty(singleValue interface{}) (prop core.Property, err error) {
	defer func() {
		if r := recover(); r != nil && r != "invalid type" {
			prop = nil
			err = p.errIncompatibleValue(singleValue)
		}
	}()

	switch p.Attribute().Type() {
	case core.TypeString:
		prop = NewString(p.Attribute().NewElementAttribute(), p)
	case core.TypeInteger:
		prop = NewInteger(p.Attribute().NewElementAttribute(), p)
	case core.TypeDecimal:
		prop = NewDecimal(p.Attribute().NewElementAttribute(), p)
	case core.TypeBoolean:
		prop = NewBoolean(p.Attribute().NewElementAttribute(), p)
	case core.TypeReference:
		prop = NewReference(p.Attribute().NewElementAttribute(), p)
	case core.TypeBinary:
		prop = NewBinary(p.Attribute().NewElementAttribute(), p)
	case core.TypeDateTime:
		prop = NewDateTime(p.Attribute().NewElementAttribute(), p)
	case core.TypeComplex:
		prop = NewComplex(p.Attribute().NewElementAttribute(annotations.StateSummary), p)
	default:
		panic("invalid type")
	}

	if singleValue != nil {
		err = prop.Replace(singleValue)
	}

	return
}

func (p *multiValuedProperty) errIncompatibleValue(value interface{}) error {
	return errors.InvalidValue("%v is incompatible with attribute '%s'", value, p.attr.Path())
}

func (p *multiValuedProperty) errIncompatibleOp() error {
	return errors.Internal("incompatible operation")
}
