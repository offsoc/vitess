/*
Copyright 2019 The Vitess Authors.

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

package vindexes

import (
	"context"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"vitess.io/vitess/go/sqltypes"
	"vitess.io/vitess/go/vt/key"
)

var reverseBits SingleColumn

func init() {
	hv, err := CreateVindex("reverse_bits", "rr", map[string]string{})
	if err != nil {
		panic(err)
	}
	unknownParams := hv.(ParamValidating).UnknownParams()
	if len(unknownParams) > 0 {
		panic("reverse_bits test init: expected 0 unknown params")
	}
	reverseBits = hv.(SingleColumn)
}

func reverseBitsCreateVindexTestCase(
	testName string,
	vindexParams map[string]string,
	expectErr error,
	expectUnknownParams []string,
) createVindexTestCase {
	return createVindexTestCase{
		testName: testName,

		vindexType:   "reverse_bits",
		vindexName:   "reverse_bits",
		vindexParams: vindexParams,

		expectCost:          1,
		expectErr:           expectErr,
		expectIsUnique:      true,
		expectNeedsVCursor:  false,
		expectString:        "reverse_bits",
		expectUnknownParams: expectUnknownParams,
	}
}

func TestReverseBitsCreateVindex(t *testing.T) {
	cases := []createVindexTestCase{
		reverseBitsCreateVindexTestCase(
			"no params",
			nil,
			nil,
			nil,
		),
		reverseBitsCreateVindexTestCase(
			"empty params",
			map[string]string{},
			nil,
			nil,
		),
		reverseBitsCreateVindexTestCase(
			"unknown params",
			map[string]string{
				"hello": "world",
			},
			nil,
			[]string{"hello"},
		),
	}

	testCreateVindexes(t, cases)
}

func TestReverseBitsMap(t *testing.T) {
	got, err := reverseBits.Map(context.Background(), nil, []sqltypes.Value{
		sqltypes.NewInt64(1),
		sqltypes.NewInt64(2),
		sqltypes.NewInt64(3),
		sqltypes.NULL,
		sqltypes.NewInt64(4),
		sqltypes.NewInt64(5),
		sqltypes.NewInt64(6),
	})
	require.NoError(t, err)
	want := []key.ShardDestination{
		key.DestinationKeyspaceID([]byte("\x80\x00\x00\x00\x00\x00\x00\x00")),
		key.DestinationKeyspaceID([]byte("@\x00\x00\x00\x00\x00\x00\x00")),
		key.DestinationKeyspaceID([]byte("\xc0\x00\x00\x00\x00\x00\x00\x00")),
		key.DestinationNone{},
		key.DestinationKeyspaceID([]byte(" \x00\x00\x00\x00\x00\x00\x00")),
		key.DestinationKeyspaceID([]byte("\xa0\x00\x00\x00\x00\x00\x00\x00")),
		key.DestinationKeyspaceID([]byte("`\x00\x00\x00\x00\x00\x00\x00")),
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Map(): %#v, want %+v", got, want)
	}
}

func TestReverseBitsVerify(t *testing.T) {
	ids := []sqltypes.Value{sqltypes.NewInt64(1), sqltypes.NewInt64(2)}
	ksids := [][]byte{[]byte("\x80\x00\x00\x00\x00\x00\x00\x00"), []byte("\x80\x00\x00\x00\x00\x00\x00\x00")}
	got, err := reverseBits.Verify(context.Background(), nil, ids, ksids)
	if err != nil {
		t.Fatal(err)
	}
	want := []bool{true, false}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("reverseBits.Verify: %v, want %v", got, want)
	}

	// Failure test
	_, err = reverseBits.Verify(context.Background(), nil, []sqltypes.Value{sqltypes.NewVarBinary("aa")}, [][]byte{nil})
	require.EqualError(t, err, "cannot parse uint64 from \"aa\"")
}

func TestReverseBitsReverseMap(t *testing.T) {
	got, err := reverseBits.(Reversible).ReverseMap(nil, [][]byte{[]byte("\x80\x00\x00\x00\x00\x00\x00\x00")})
	require.NoError(t, err)
	want := []sqltypes.Value{sqltypes.NewUint64(uint64(1))}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ReverseMap(): %v, want %v", got, want)
	}
}

func TestReverseBitsReverseMapNeg(t *testing.T) {
	_, err := reverseBits.(Reversible).ReverseMap(nil, [][]byte{[]byte("\x80\x00\x00\x00\x00\x00\x00\x00\x80\x00\x00\x00\x00\x00\x00\x00")})
	want := "invalid keyspace id: 80000000000000008000000000000000"
	if err.Error() != want {
		t.Error(err)
	}
}
