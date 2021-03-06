package sexpr

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strconv"
	"strings"
	"text/scanner"
)

func Unmarshal(data []byte, out interface{}) (err error) {
	lex := &lexer{scan: scanner.Scanner{Mode: scanner.GoTokens}}
	lex.scan.Init(bytes.NewReader(data))
	lex.next()
	defer func() {
		if x := recover(); x != nil {
			err = fmt.Errorf("error at %s: %v", lex.scan.Position, x)
		}
	}()
	read(lex, reflect.ValueOf(out).Elem())
	return nil
}

type Decoder struct {
	lex *lexer
}

func (d *Decoder) Decode(v interface{}) (err error) {
	d.lex.next()
	defer func() {
		if x := recover(); x != nil {
			err = fmt.Errorf("error at %s: %v", d.lex.scan.Position, x)
		}
	}()
	read(d.lex, reflect.ValueOf(v).Elem())
	return nil

}

type Token interface{}
type Symbol struct{ Value string }
type String struct{ Value string }
type Int struct{ Value int }
type StartList struct{}
type EndList struct{}

func (d *Decoder) Token() (Token, error) {
	d.lex.next()
	switch d.lex.token {
	case scanner.Ident:
		return Symbol{d.lex.text()}, nil
	case scanner.String:
		s, _ := strconv.Unquote(d.lex.text())
		return String{s}, nil
	case scanner.Int:
		i, _ := strconv.Atoi(d.lex.text())
		return Int{i}, nil
	case '(':
		return StartList{}, nil
	case ')':
		return EndList{}, nil
	case scanner.EOF:
		return nil, errors.New("EOF")
	}
	panic(fmt.Sprintf("unexpected token %q", d.lex.text()))
}

func NewDecoder(r io.Reader) *Decoder {
	lex := &lexer{
		scan: scanner.Scanner{Mode: scanner.GoTokens},
	}
	lex.scan.Init(r)
	return &Decoder{lex: lex}
}

type lexer struct {
	scan  scanner.Scanner
	token rune
}

func (lex *lexer) next()        { lex.token = lex.scan.Scan() }
func (lex *lexer) text() string { return lex.scan.TokenText() }

func (lex *lexer) consume(want rune) {
	if lex.token != want {
		panic(fmt.Sprintf("got %q, want %q", lex.text(), want))
	}
	lex.next()
}

func read(lex *lexer, v reflect.Value) {
	switch lex.token {
	case scanner.Ident:
		if lex.text() == "nil" {
			v.Set(reflect.Zero(v.Type()))
			lex.next()
			return
		} else if lex.text() == "t" {
			v.SetBool(true)
			lex.next()
			return
		}
	case scanner.String:
		s, _ := strconv.Unquote(lex.text())
		v.SetString(s)
		lex.next()
		return
	case scanner.Int:
		i, _ := strconv.Atoi(lex.text())
		v.SetInt(int64(i))
		lex.next()
		return
	case scanner.Float:
		f, _ := strconv.ParseFloat(lex.text(), 64)
		v.SetFloat(f)
		lex.next()
		return
	case '(':
		lex.next()
		readList(lex, v)
		lex.next()
		return
	}
	panic(fmt.Sprintf("unexpected token %q", lex.text()))
}

func readList(lex *lexer, v reflect.Value) {
	switch v.Kind() {
	case reflect.Array:
		for i := 0; !endList(lex); i++ {
			read(lex, v.Index(i))
		}

	case reflect.Slice:
		for !endList(lex) {
			item := reflect.New(v.Type().Elem()).Elem()
			read(lex, item)
			v.Set(reflect.Append(v, item))
		}

	case reflect.Struct:
		for !endList(lex) {
			lex.consume('(')
			if lex.token != scanner.Ident {
				panic(fmt.Sprintf("got token %q, want field name", lex.text()))
			}
			name := lex.text()
			lex.next()
			read(lex, v.FieldByName(name))
			lex.consume(')')
		}

	case reflect.Map:
		v.Set(reflect.MakeMap(v.Type()))
		for !endList(lex) {
			lex.consume('(')
			key := reflect.New(v.Type().Key()).Elem()
			read(lex, key)
			value := reflect.New(v.Type().Elem()).Elem()
			read(lex, value)
			v.SetMapIndex(key, value)
			lex.consume(')')
		}

	case reflect.Interface:
		typStr, _ := strconv.Unquote(lex.text())
		typ := asType(typStr)
		lex.next()
		value := reflect.New(typ).Elem()
		read(lex, value)
		v.Set(value)

	default:
		panic(fmt.Sprintf("cannot decode list into %v", v.Type()))
	}
}

var atomTypes = map[string]reflect.Type{
	"int":    reflect.TypeOf(int(0)),
	"uint":   reflect.TypeOf(uint(0)),
	"float":  reflect.TypeOf(float64(0)),
	"bool":   reflect.TypeOf(false),
	"string": reflect.TypeOf(""),
}

func asType(typ string) reflect.Type {
	if t, ok := atomTypes[typ]; ok {
		return t
	}
	if strings.HasPrefix(typ, "[]") {
		return reflect.SliceOf(asType(typ[2:]))
	}
	if typ[0] == '[' {
		i, j := 0, strings.IndexRune(typ, ']')
		count, _ := strconv.Atoi(typ[i+1 : j])
		elem := typ[j+1:]
		return reflect.ArrayOf(count, asType(elem))
	}
	if strings.HasPrefix(typ, "map") {
		i, j := strings.IndexRune(typ, '['), strings.IndexRune(typ, ']')
		key := typ[i+1 : j]
		elem := typ[j+1:]
		return reflect.MapOf(asType(key), asType(elem))
	}
	panic(fmt.Sprintf("unknown type %q", typ))
}

func endList(lex *lexer) bool {
	switch lex.token {
	case scanner.EOF:
		panic("end of file")
	case ')':
		return true
	}
	return false
}
