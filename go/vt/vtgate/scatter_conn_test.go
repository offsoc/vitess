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

package vtgate

import (
	"fmt"
	"testing"

	"github.com/aws/smithy-go/ptr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"vitess.io/vitess/go/mysql/sqlerror"
	"vitess.io/vitess/go/sqltypes"
	"vitess.io/vitess/go/test/utils"
	"vitess.io/vitess/go/vt/discovery"
	"vitess.io/vitess/go/vt/key"
	"vitess.io/vitess/go/vt/log"
	querypb "vitess.io/vitess/go/vt/proto/query"
	topodatapb "vitess.io/vitess/go/vt/proto/topodata"
	vtgatepb "vitess.io/vitess/go/vt/proto/vtgate"
	vtrpcpb "vitess.io/vitess/go/vt/proto/vtrpc"
	"vitess.io/vitess/go/vt/srvtopo"
	"vitess.io/vitess/go/vt/vterrors"
	econtext "vitess.io/vitess/go/vt/vtgate/executorcontext"
)

// This file uses the sandbox_test framework.

func TestExecuteFailOnAutocommit(t *testing.T) {
	ctx := utils.LeakCheckContext(t)

	createSandbox("TestExecuteFailOnAutocommit")
	hc := discovery.NewFakeHealthCheck(nil)
	sc := newTestScatterConn(ctx, hc, newSandboxForCells(ctx, []string{"aa"}), "aa")
	sbc0 := hc.AddTestTablet("aa", "0", 1, "TestExecuteFailOnAutocommit", "0", topodatapb.TabletType_PRIMARY, true, 1, nil)
	sbc1 := hc.AddTestTablet("aa", "1", 1, "TestExecuteFailOnAutocommit", "1", topodatapb.TabletType_PRIMARY, true, 1, nil)

	rss := []*srvtopo.ResolvedShard{
		{
			Target: &querypb.Target{
				Keyspace:   "TestExecuteFailOnAutocommit",
				Shard:      "0",
				TabletType: topodatapb.TabletType_PRIMARY,
			},
			Gateway: sbc0,
		},
		{
			Target: &querypb.Target{
				Keyspace:   "TestExecuteFailOnAutocommit",
				Shard:      "1",
				TabletType: topodatapb.TabletType_PRIMARY,
			},
			Gateway: sbc1,
		},
	}
	queries := []*querypb.BoundQuery{
		{
			// This will fail to go to shard. It will be rejected at vtgate.
			Sql: "query1",
			BindVariables: map[string]*querypb.BindVariable{
				"bv0": sqltypes.Int64BindVariable(0),
			},
		},
		{
			// This will go to shard.
			Sql: "query2",
			BindVariables: map[string]*querypb.BindVariable{
				"bv1": sqltypes.Int64BindVariable(1),
			},
		},
	}
	// shard 0 - has transaction
	// shard 1 - does not have transaction.
	session := &vtgatepb.Session{
		InTransaction: true,
		ShardSessions: []*vtgatepb.Session_ShardSession{
			{
				Target:        &querypb.Target{Keyspace: "TestExecuteFailOnAutocommit", Shard: "0", TabletType: topodatapb.TabletType_PRIMARY, Cell: "aa"},
				TransactionId: 123,
				TabletAlias:   nil,
			},
		},
		Autocommit: false,
	}
	_, errs := sc.ExecuteMultiShard(ctx, nil, rss, queries, econtext.NewSafeSession(session), true /*autocommit*/, false, nullResultsObserver{}, false)
	err := vterrors.Aggregate(errs)
	require.Error(t, err)
	require.Contains(t, err.Error(), "in autocommit mode, transactionID should be zero but was: 123")
	utils.MustMatch(t, 0, len(sbc0.Queries), "")
	utils.MustMatch(t, []*querypb.BoundQuery{queries[1]}, sbc1.Queries, "")
}

func TestFetchLastInsertIDResets(t *testing.T) {
	// This test verifies that the FetchLastInsertID flag is reset after a call to ExecuteMultiShard.
	ks := "TestFetchLastInsertIDResets"
	ctx := utils.LeakCheckContext(t)

	createSandbox(ks)
	hc := discovery.NewFakeHealthCheck(nil)
	sc := newTestScatterConn(ctx, hc, newSandboxForCells(ctx, []string{"aa"}), "aa")
	sbc0 := hc.AddTestTablet("aa", "0", 1, ks, "0", topodatapb.TabletType_PRIMARY, true, 1, nil)
	sbc1 := hc.AddTestTablet("aa", "1", 1, ks, "1", topodatapb.TabletType_PRIMARY, true, 1, nil)

	rss := []*srvtopo.ResolvedShard{{
		Target: &querypb.Target{
			Keyspace:   ks,
			Shard:      "0",
			TabletType: topodatapb.TabletType_PRIMARY,
		},
		Gateway: sbc0,
	}, {
		Target: &querypb.Target{
			Keyspace:   ks,
			Shard:      "1",
			TabletType: topodatapb.TabletType_PRIMARY,
		},
		Gateway: sbc1,
	}}
	queries := []*querypb.BoundQuery{{
		Sql: "query1",
		BindVariables: map[string]*querypb.BindVariable{
			"bv0": sqltypes.Int64BindVariable(0),
		},
	}, {
		Sql: "query2",
		BindVariables: map[string]*querypb.BindVariable{
			"bv1": sqltypes.Int64BindVariable(1),
		},
	}}
	tests := []struct {
		name               string
		initialSessionOpts *querypb.ExecuteOptions
		fetchLastInsertID  bool
		expectSessionNil   bool
		expectFetchLastID  *bool // nil means checkLastOptionNil, otherwise checkLastOption(*bool)
	}{
		{
			name:               "no session options, fetchLastInsertID = false",
			initialSessionOpts: nil,
			fetchLastInsertID:  false,
			expectSessionNil:   true,
			expectFetchLastID:  nil,
		},
		{
			name:               "no session options, fetchLastInsertID = true",
			initialSessionOpts: nil,
			fetchLastInsertID:  true,
			expectSessionNil:   true,

			expectFetchLastID: ptr.Bool(true),
		},
		{
			name:               "session options set, fetchLastInsertID = false",
			initialSessionOpts: &querypb.ExecuteOptions{},
			fetchLastInsertID:  false,
			expectSessionNil:   false,
			expectFetchLastID:  ptr.Bool(false),
		},
		{
			name:               "session options set, fetchLastInsertID = true",
			initialSessionOpts: &querypb.ExecuteOptions{},
			fetchLastInsertID:  true,
			expectSessionNil:   false,
			expectFetchLastID:  ptr.Bool(true),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			session := econtext.NewSafeSession(nil)
			session.Options = tt.initialSessionOpts

			checkLastOption := func(expected bool) {
				require.Equal(t, 1, len(sbc0.Options))
				options := sbc0.Options[0]
				assert.Equal(t, options.FetchLastInsertId, expected)
				sbc0.Options = nil
			}
			checkLastOptionNil := func() {
				require.Equal(t, 1, len(sbc0.Options))
				assert.Nil(t, sbc0.Options[0])
				sbc0.Options = nil
			}

			_, errs := sc.ExecuteMultiShard(ctx, nil, rss, queries, session, true /*autocommit*/, false, nullResultsObserver{}, tt.fetchLastInsertID)
			require.NoError(t, vterrors.Aggregate(errs))

			if tt.expectSessionNil {
				assert.Nil(t, session.Options)
			} else {
				assert.NotNil(t, session.Options)
				assert.Equal(t, tt.fetchLastInsertID, session.Options.FetchLastInsertId)
			}

			if tt.expectFetchLastID == nil {
				checkLastOptionNil()
			} else {
				checkLastOption(*tt.expectFetchLastID)
			}
		})
	}
}

func TestExecutePanic(t *testing.T) {
	ctx := utils.LeakCheckContext(t)

	createSandbox("TestExecutePanic")
	hc := discovery.NewFakeHealthCheck(nil)
	sc := newTestScatterConn(ctx, hc, newSandboxForCells(ctx, []string{"aa"}), "aa")
	sbc0 := hc.AddTestTablet("aa", "0", 1, "TestExecutePanic", "0", topodatapb.TabletType_PRIMARY, true, 1, nil)
	sbc1 := hc.AddTestTablet("aa", "1", 1, "TestExecutePanic", "1", topodatapb.TabletType_PRIMARY, true, 1, nil)
	sbc0.SetPanic(42)
	sbc1.SetPanic(42)
	rss := []*srvtopo.ResolvedShard{
		{
			Target: &querypb.Target{
				Keyspace:   "TestExecutePanic",
				Shard:      "0",
				TabletType: topodatapb.TabletType_PRIMARY,
			},
			Gateway: sbc0,
		},
		{
			Target: &querypb.Target{
				Keyspace:   "TestExecutePanic",
				Shard:      "1",
				TabletType: topodatapb.TabletType_PRIMARY,
			},
			Gateway: sbc1,
		},
	}
	queries := []*querypb.BoundQuery{
		{
			// This will fail to go to shard. It will be rejected at vtgate.
			Sql: "query1",
			BindVariables: map[string]*querypb.BindVariable{
				"bv0": sqltypes.Int64BindVariable(0),
			},
		},
		{
			// This will go to shard.
			Sql: "query2",
			BindVariables: map[string]*querypb.BindVariable{
				"bv1": sqltypes.Int64BindVariable(1),
			},
		},
	}
	// shard 0 - has transaction
	// shard 1 - does not have transaction.
	session := &vtgatepb.Session{
		InTransaction: true,
		ShardSessions: []*vtgatepb.Session_ShardSession{
			{
				Target:        &querypb.Target{Keyspace: "TestExecutePanic", Shard: "0", TabletType: topodatapb.TabletType_PRIMARY, Cell: "aa"},
				TransactionId: 123,
				TabletAlias:   nil,
			},
		},
		Autocommit: false,
	}

	original := log.Errorf
	defer func() {
		log.Errorf = original
	}()

	var logMessage string
	log.Errorf = func(format string, args ...any) {
		logMessage = fmt.Sprintf(format, args...)
	}

	assert.Panics(t, func() {
		_, _ = sc.ExecuteMultiShard(ctx, nil, rss, queries, econtext.NewSafeSession(session), true /*autocommit*/, false, nullResultsObserver{}, false)
	})
	require.Contains(t, logMessage, "(*ScatterConn).multiGoTransaction")
}

func TestReservedOnMultiReplica(t *testing.T) {
	ctx := utils.LeakCheckContext(t)

	keyspace := "keyspace"
	createSandbox(keyspace)
	hc := discovery.NewFakeHealthCheck(nil)
	sc := newTestScatterConn(ctx, hc, newSandboxForCells(ctx, []string{"aa"}), "aa")
	sbc0_1 := hc.AddTestTablet("aa", "0", 1, keyspace, "0", topodatapb.TabletType_REPLICA, true, 1, nil)
	sbc0_2 := hc.AddTestTablet("aa", "2", 1, keyspace, "0", topodatapb.TabletType_REPLICA, true, 1, nil)
	//	sbc1 := hc.AddTestTablet("aa", "1", 1, keyspace, "1", topodatapb.TabletType_REPLICA, true, 1, nil)

	// empty results
	sbc0_1.SetResults([]*sqltypes.Result{{}})
	sbc0_2.SetResults([]*sqltypes.Result{{}})

	res := srvtopo.NewResolver(newSandboxForCells(ctx, []string{"aa"}), sc.gateway, "aa")

	session := econtext.NewSafeSession(&vtgatepb.Session{InTransaction: false, InReservedConn: true})
	destinations := []key.ShardDestination{key.DestinationShard("0")}
	for i := 0; i < 10; i++ {
		executeOnShards(t, ctx, res, keyspace, sc, session, destinations)
		assert.EqualValues(t, 1, sbc0_1.ReserveCount.Load()+sbc0_2.ReserveCount.Load(), "sbc0 reserve count")
		assert.EqualValues(t, 0, sbc0_1.BeginCount.Load()+sbc0_2.BeginCount.Load(), "sbc0 begin count")
	}
}

func TestReservedBeginTableDriven(t *testing.T) {
	ctx := utils.LeakCheckContext(t)

	type testAction struct {
		transaction, reserved    bool
		shards                   []string
		sbc0Reserve, sbc1Reserve int64
		sbc0Begin, sbc1Begin     int64
	}
	type testCase struct {
		name    string
		actions []testAction
	}

	tests := []testCase{{
		name: "begin",
		actions: []testAction{
			{
				shards:      []string{"0"},
				transaction: true,
				sbc0Begin:   1,
			}, {
				shards:      []string{"0", "1"},
				transaction: true,
				sbc1Begin:   1,
			}, {
				shards:      []string{"0", "1"},
				transaction: true,
				// nothing needs to be done
			}},
	}, {
		name: "reserve",
		actions: []testAction{
			{
				shards:      []string{"1"},
				reserved:    true,
				sbc1Reserve: 1,
			}, {
				shards:      []string{"0", "1"},
				reserved:    true,
				sbc0Reserve: 1,
			}, {
				shards:   []string{"0", "1"},
				reserved: true,
				// nothing needs to be done
			}},
	}, {
		name: "reserve everywhere",
		actions: []testAction{
			{
				shards:      []string{"0", "1"},
				reserved:    true,
				sbc0Reserve: 1,
				sbc1Reserve: 1,
			}},
	}, {
		name: "begin then reserve",
		actions: []testAction{
			{
				shards:      []string{"0"},
				transaction: true,
				sbc0Begin:   1,
			}, {
				shards:      []string{"0", "1"},
				transaction: true,
				reserved:    true,
				sbc0Reserve: 1,
				sbc1Reserve: 1,
				sbc1Begin:   1,
			}},
	}, {
		name: "reserve then begin",
		actions: []testAction{
			{
				shards:      []string{"1"},
				reserved:    true,
				sbc1Reserve: 1,
			}, {
				shards:      []string{"0"},
				transaction: true,
				reserved:    true,
				sbc0Reserve: 1,
				sbc0Begin:   1,
			}, {
				shards:      []string{"0", "1"},
				transaction: true,
				reserved:    true,
				sbc1Begin:   1,
			}},
	}, {
		name: "reserveBegin",
		actions: []testAction{
			{
				shards:      []string{"1"},
				transaction: true,
				reserved:    true,
				sbc1Reserve: 1,
				sbc1Begin:   1,
			}, {
				shards:      []string{"0"},
				transaction: true,
				reserved:    true,
				sbc0Reserve: 1,
				sbc0Begin:   1,
			}, {
				shards:      []string{"0", "1"},
				transaction: true,
				reserved:    true,
				// nothing needs to be done
			}},
	}, {
		name: "reserveBegin everywhere",
		actions: []testAction{
			{
				shards:      []string{"0", "1"},
				transaction: true,
				reserved:    true,
				sbc0Reserve: 1,
				sbc0Begin:   1,
				sbc1Reserve: 1,
				sbc1Begin:   1,
			}},
	}}
	for _, test := range tests {
		keyspace := "keyspace"
		createSandbox(keyspace)
		hc := discovery.NewFakeHealthCheck(nil)
		sc := newTestScatterConn(ctx, hc, newSandboxForCells(ctx, []string{"aa"}), "aa")
		sbc0 := hc.AddTestTablet("aa", "0", 1, keyspace, "0", topodatapb.TabletType_REPLICA, true, 1, nil)
		sbc1 := hc.AddTestTablet("aa", "1", 1, keyspace, "1", topodatapb.TabletType_REPLICA, true, 1, nil)

		// empty results
		sbc0.SetResults([]*sqltypes.Result{{}})
		sbc1.SetResults([]*sqltypes.Result{{}})

		res := srvtopo.NewResolver(newSandboxForCells(ctx, []string{"aa"}), sc.gateway, "aa")

		t.Run(test.name, func(t *testing.T) {
			session := econtext.NewSafeSession(&vtgatepb.Session{})
			for _, action := range test.actions {
				session.Session.InTransaction = action.transaction
				session.Session.InReservedConn = action.reserved
				var destinations []key.ShardDestination
				for _, shard := range action.shards {
					destinations = append(destinations, key.DestinationShard(shard))
				}
				executeOnShards(t, ctx, res, keyspace, sc, session, destinations)
				assert.EqualValues(t, action.sbc0Reserve, sbc0.ReserveCount.Load(), "sbc0 reserve count")
				assert.EqualValues(t, action.sbc0Begin, sbc0.BeginCount.Load(), "sbc0 begin count")
				assert.EqualValues(t, action.sbc1Reserve, sbc1.ReserveCount.Load(), "sbc1 reserve count")
				assert.EqualValues(t, action.sbc1Begin, sbc1.BeginCount.Load(), "sbc1 begin count")
				sbc0.BeginCount.Store(0)
				sbc0.ReserveCount.Store(0)
				sbc1.BeginCount.Store(0)
				sbc1.ReserveCount.Store(0)
			}
		})
	}
}

func TestReservedConnFail(t *testing.T) {
	ctx := utils.LeakCheckContext(t)

	keyspace := "keyspace"
	createSandbox(keyspace)
	hc := discovery.NewFakeHealthCheck(nil)
	sc := newTestScatterConn(ctx, hc, newSandboxForCells(ctx, []string{"aa"}), "aa")
	sbc0 := hc.AddTestTablet("aa", "0", 1, keyspace, "0", topodatapb.TabletType_REPLICA, true, 1, nil)
	_ = hc.AddTestTablet("aa", "1", 1, keyspace, "1", topodatapb.TabletType_REPLICA, true, 1, nil)
	res := srvtopo.NewResolver(newSandboxForCells(ctx, []string{"aa"}), sc.gateway, "aa")

	session := econtext.NewSafeSession(&vtgatepb.Session{InTransaction: false, InReservedConn: true})
	destinations := []key.ShardDestination{key.DestinationShard("0")}

	executeOnShards(t, ctx, res, keyspace, sc, session, destinations)
	assert.Equal(t, 1, len(session.ShardSessions))
	oldRId := session.Session.ShardSessions[0].ReservedId

	sbc0.EphemeralShardErr = sqlerror.NewSQLError(sqlerror.CRServerGone, sqlerror.SSUnknownSQLState, "lost connection")
	_ = executeOnShardsReturnsErr(t, ctx, res, keyspace, sc, session, destinations)
	assert.Equal(t, 3, len(sbc0.Queries), "1 for the successful run, one for the failed attempt, and one for the retry")
	require.Equal(t, 1, len(session.ShardSessions))
	assert.NotEqual(t, oldRId, session.Session.ShardSessions[0].ReservedId, "should have recreated a reserved connection since the last connection was lost")
	oldRId = session.Session.ShardSessions[0].ReservedId

	sbc0.Queries = nil
	sbc0.EphemeralShardErr = sqlerror.NewSQLError(sqlerror.ERQueryInterrupted, sqlerror.SSUnknownSQLState, "transaction 123 not found")
	_ = executeOnShardsReturnsErr(t, ctx, res, keyspace, sc, session, destinations)
	assert.Equal(t, 2, len(sbc0.Queries), "one for the failed attempt, and one for the retry")
	require.Equal(t, 1, len(session.ShardSessions))
	assert.NotEqual(t, oldRId, session.Session.ShardSessions[0].ReservedId, "should have recreated a reserved connection since the last connection was lost")
	oldRId = session.Session.ShardSessions[0].ReservedId

	sbc0.Queries = nil
	sbc0.EphemeralShardErr = sqlerror.NewSQLError(sqlerror.ERQueryInterrupted, sqlerror.SSUnknownSQLState, "transaction 123 ended at 2020-01-20")
	_ = executeOnShardsReturnsErr(t, ctx, res, keyspace, sc, session, destinations)
	assert.Equal(t, 2, len(sbc0.Queries), "one for the failed attempt, and one for the retry")
	require.Equal(t, 1, len(session.ShardSessions))
	assert.NotEqual(t, oldRId, session.Session.ShardSessions[0].ReservedId, "should have recreated a reserved connection since the last connection was lost")
	oldRId = session.Session.ShardSessions[0].ReservedId

	sbc0.Queries = nil
	sbc0.EphemeralShardErr = sqlerror.NewSQLError(sqlerror.ERQueryInterrupted, sqlerror.SSUnknownSQLState, "transaction 123 in use: for tx killer rollback")
	_ = executeOnShardsReturnsErr(t, ctx, res, keyspace, sc, session, destinations)
	assert.Equal(t, 2, len(sbc0.Queries), "one for the failed attempt, and one for the retry")
	require.Equal(t, 1, len(session.ShardSessions))
	assert.NotEqual(t, oldRId, session.Session.ShardSessions[0].ReservedId, "should have recreated a reserved connection since the last connection was lost")
	oldRId = session.Session.ShardSessions[0].ReservedId

	sbc0.Queries = nil
	sbc0.EphemeralShardErr = vterrors.New(vtrpcpb.Code_CLUSTER_EVENT, "operation not allowed in state NOT_SERVING during query: query1")
	_ = executeOnShardsReturnsErr(t, ctx, res, keyspace, sc, session, destinations)
	assert.Equal(t, 2, len(sbc0.Queries), "one for the failed attempt, and one for the retry")
	require.Equal(t, 1, len(session.ShardSessions))
	assert.NotEqual(t, oldRId, session.Session.ShardSessions[0].ReservedId, "should have recreated a reserved connection since the last connection was lost")
	oldRId = session.Session.ShardSessions[0].ReservedId

	sbc0.Queries = nil
	sbc0.EphemeralShardErr = vterrors.New(vtrpcpb.Code_FAILED_PRECONDITION, "invalid tablet type: REPLICA, want: PRIMARY")
	_ = executeOnShardsReturnsErr(t, ctx, res, keyspace, sc, session, destinations)
	assert.Equal(t, 2, len(sbc0.Queries), "one for the failed attempt, and one for the retry")
	require.Equal(t, 1, len(session.ShardSessions))
	assert.NotEqual(t, oldRId, session.Session.ShardSessions[0].ReservedId, "should have recreated a reserved connection since the last connection was lost")
	oldRId = session.Session.ShardSessions[0].ReservedId
	oldAlias := session.Session.ShardSessions[0].TabletAlias

	// Test Setup
	tablet0 := sbc0.Tablet()
	ths := hc.GetHealthyTabletStats(&querypb.Target{
		Keyspace:   tablet0.GetKeyspace(),
		Shard:      tablet0.GetShard(),
		TabletType: tablet0.GetType(),
	})
	sbc0Th := ths[0]
	sbc0Th.Serving = false
	sbc0.NotServing = true
	sbc0Rep := hc.AddTestTablet("aa", "0", 2, keyspace, "0", topodatapb.TabletType_REPLICA, true, 1, nil)

	sbc0.Queries = nil
	sbc0.ExecCount.Store(0)
	_ = executeOnShardsReturnsErr(t, ctx, res, keyspace, sc, session, destinations)
	assert.EqualValues(t, 1, sbc0.ExecCount.Load(), "first attempt should be made on original tablet")
	assert.EqualValues(t, 0, len(sbc0.Queries), "no query should be executed on it")
	assert.Equal(t, 1, len(sbc0Rep.Queries), "this attempt on new healthy tablet should pass")
	require.Equal(t, 1, len(session.ShardSessions))
	assert.NotEqual(t, oldRId, session.Session.ShardSessions[0].ReservedId, "should have recreated a reserved connection since the last connection was lost")
	assert.NotEqual(t, oldAlias, session.Session.ShardSessions[0].TabletAlias, "tablet alias should have changed as this is a different tablet")
	oldRId = session.Session.ShardSessions[0].ReservedId
	oldAlias = session.Session.ShardSessions[0].TabletAlias

	// Test Setup
	tablet0Rep := sbc0Rep.Tablet()
	newThs := hc.GetHealthyTabletStats(&querypb.Target{
		Keyspace:   tablet0Rep.GetKeyspace(),
		Shard:      tablet0Rep.GetShard(),
		TabletType: tablet0Rep.GetType(),
	})
	sbc0RepTh := newThs[0]
	sbc0RepTh.Target = &querypb.Target{
		Keyspace:   tablet0Rep.GetKeyspace(),
		Shard:      tablet0Rep.GetShard(),
		TabletType: topodatapb.TabletType_SPARE,
	}
	sbc0Rep.Tablet().Type = topodatapb.TabletType_SPARE
	sbc0Th.Serving = true
	sbc0.NotServing = false
	sbc0.ExecCount.Store(0)

	sbc0Rep.Queries = nil
	sbc0Rep.ExecCount.Store(0)
	_ = executeOnShardsReturnsErr(t, ctx, res, keyspace, sc, session, destinations)
	assert.EqualValues(t, 1, sbc0Rep.ExecCount.Load(), "first attempt should be made on the changed tablet type")
	assert.EqualValues(t, 0, len(sbc0Rep.Queries), "no query should be executed on it")
	assert.Equal(t, 1, len(sbc0.Queries), "this attempt should pass as it is on new healthy tablet and matches the target")
	require.Equal(t, 1, len(session.ShardSessions))
	assert.NotEqual(t, oldRId, session.Session.ShardSessions[0].ReservedId, "should have recreated a reserved connection since the last connection was lost")
	assert.NotEqual(t, oldAlias, session.Session.ShardSessions[0].TabletAlias, "tablet alias should have changed as this is a different tablet")
}

func TestIsConnClosed(t *testing.T) {
	var testCases = []struct {
		name      string
		err       error
		conClosed bool
	}{{
		"server gone",
		sqlerror.NewSQLError(sqlerror.CRServerGone, sqlerror.SSNetError, ""),
		true,
	}, {
		"connection lost",
		sqlerror.NewSQLError(sqlerror.CRServerLost, sqlerror.SSNetError, ""),
		true,
	}, {
		"tx ended",
		sqlerror.NewSQLError(sqlerror.ERQueryInterrupted, sqlerror.SSUnknownSQLState, "transaction 111 ended at ..."),
		true,
	}, {
		"tx not found",
		sqlerror.NewSQLError(sqlerror.ERQueryInterrupted, sqlerror.SSUnknownSQLState, "transaction 111 not found ..."),
		true,
	}, {
		"tx not found missing tx id",
		sqlerror.NewSQLError(sqlerror.ERQueryInterrupted, sqlerror.SSUnknownSQLState, "transaction not found"),
		false,
	}, {
		"tx getting killed by tx killer",
		sqlerror.NewSQLError(sqlerror.ERQueryInterrupted, sqlerror.SSUnknownSQLState, "transaction 111 in use: for tx killer rollback"),
		true,
	}}

	for _, tCase := range testCases {
		t.Run(tCase.name, func(t *testing.T) {
			assert.Equal(t, tCase.conClosed, wasConnectionClosed(tCase.err))
		})
	}
}
