package api

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"sync"
	"time"

	"github.com/linkerd/linkerd2/controller/k8s"
	pb "github.com/linkerd/linkerd2/viz/metrics-api/gen/viz"
	promv1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"google.golang.org/grpc"
)

// MockAPIClient satisfies the Public API's gRPC interfaces (public.APIClient).
type MockAPIClient struct {
	ErrorToReturn                error
	ListPodsResponseToReturn     *pb.ListPodsResponse
	ListServicesResponseToReturn *pb.ListServicesResponse
	StatSummaryResponseToReturn  *pb.StatSummaryResponse
	GatewaysResponseToReturn     *pb.GatewaysResponse
	TopRoutesResponseToReturn    *pb.TopRoutesResponse
	EdgesResponseToReturn        *pb.EdgesResponse
	SelfCheckResponseToReturn    *pb.SelfCheckResponse
}

// StatSummary provides a mock of a Public API method.
func (c *MockAPIClient) StatSummary(ctx context.Context, in *pb.StatSummaryRequest, opts ...grpc.CallOption) (*pb.StatSummaryResponse, error) {
	return c.StatSummaryResponseToReturn, c.ErrorToReturn
}

// Gateways provides a mock of a Public API method.
func (c *MockAPIClient) Gateways(ctx context.Context, in *pb.GatewaysRequest, opts ...grpc.CallOption) (*pb.GatewaysResponse, error) {
	return c.GatewaysResponseToReturn, c.ErrorToReturn
}

// TopRoutes provides a mock of a Public API method.
func (c *MockAPIClient) TopRoutes(ctx context.Context, in *pb.TopRoutesRequest, opts ...grpc.CallOption) (*pb.TopRoutesResponse, error) {
	return c.TopRoutesResponseToReturn, c.ErrorToReturn
}

// Edges provides a mock of a Public API method.
func (c *MockAPIClient) Edges(ctx context.Context, in *pb.EdgesRequest, opts ...grpc.CallOption) (*pb.EdgesResponse, error) {
	return c.EdgesResponseToReturn, c.ErrorToReturn
}

// ListPods provides a mock of a Public API method.
func (c *MockAPIClient) ListPods(ctx context.Context, in *pb.ListPodsRequest, opts ...grpc.CallOption) (*pb.ListPodsResponse, error) {
	return c.ListPodsResponseToReturn, c.ErrorToReturn
}

// ListServices provides a mock of a Public API method.
func (c *MockAPIClient) ListServices(ctx context.Context, in *pb.ListServicesRequest, opts ...grpc.CallOption) (*pb.ListServicesResponse, error) {
	return c.ListServicesResponseToReturn, c.ErrorToReturn
}

// SelfCheck provides a mock of a Public API method.
func (c *MockAPIClient) SelfCheck(ctx context.Context, in *pb.SelfCheckRequest, _ ...grpc.CallOption) (*pb.SelfCheckResponse, error) {
	return c.SelfCheckResponseToReturn, c.ErrorToReturn
}

//
// Prometheus client
//

// MockProm satisfies the promv1.API interface for testing.
// TODO: move this into something shared under /controller, or into /pkg
type MockProm struct {
	Res             model.Value
	QueriesExecuted []string // expose the queries our Mock Prometheus receives, to test query generation
	rwLock          sync.Mutex
}

// PodCounts is a test helper struct that is used for representing data in a
// StatTable.PodGroup.Row.
type PodCounts struct {
	Status      string
	MeshedPods  uint64
	RunningPods uint64
	FailedPods  uint64
	Errors      map[string]*pb.PodErrors
}

// Query performs a query for the given time.
func (m *MockProm) Query(ctx context.Context, query string, ts time.Time) (model.Value, promv1.Warnings, error) {
	m.rwLock.Lock()
	defer m.rwLock.Unlock()
	m.QueriesExecuted = append(m.QueriesExecuted, query)
	return m.Res, nil, nil
}

// QueryRange performs a query for the given range.
func (m *MockProm) QueryRange(ctx context.Context, query string, r promv1.Range) (model.Value, promv1.Warnings, error) {
	m.rwLock.Lock()
	defer m.rwLock.Unlock()
	m.QueriesExecuted = append(m.QueriesExecuted, query)
	return m.Res, nil, nil
}

// AlertManagers returns an overview of the current state of the Prometheus alert
// manager discovery.
func (m *MockProm) AlertManagers(ctx context.Context) (promv1.AlertManagersResult, error) {
	return promv1.AlertManagersResult{}, nil
}

// Alerts returns a list of all active alerts.
func (m *MockProm) Alerts(ctx context.Context) (promv1.AlertsResult, error) {
	return promv1.AlertsResult{}, nil
}

// CleanTombstones removes the deleted data from disk and cleans up the existing
// tombstones.
func (m *MockProm) CleanTombstones(ctx context.Context) error {
	return nil
}

// Config returns the current Prometheus configuration.
func (m *MockProm) Config(ctx context.Context) (promv1.ConfigResult, error) {
	return promv1.ConfigResult{}, nil
}

// DeleteSeries deletes data for a selection of series in a time range.
func (m *MockProm) DeleteSeries(ctx context.Context, matches []string, startTime time.Time, endTime time.Time) error {
	return nil
}

// Flags returns the flag values that Prometheus was launched with.
func (m *MockProm) Flags(ctx context.Context) (promv1.FlagsResult, error) {
	return promv1.FlagsResult{}, nil
}

// LabelValues performs a query for the values of the given label.
func (m *MockProm) LabelValues(ctx context.Context, label string, startTime time.Time, endTime time.Time) (model.LabelValues, promv1.Warnings, error) {
	return nil, nil, nil
}

// Series finds series by label matchers.
func (m *MockProm) Series(ctx context.Context, matches []string, startTime time.Time, endTime time.Time) ([]model.LabelSet, promv1.Warnings, error) {
	return nil, nil, nil
}

// Snapshot creates a snapshot of all current data into
// snapshots/<datetime>-<rand> under the TSDB's data directory and returns the
// directory as response.
func (m *MockProm) Snapshot(ctx context.Context, skipHead bool) (promv1.SnapshotResult, error) {
	return promv1.SnapshotResult{}, nil
}

// Targets returns an overview of the current state of the Prometheus target
// discovery.
func (m *MockProm) Targets(ctx context.Context) (promv1.TargetsResult, error) {
	return promv1.TargetsResult{}, nil
}

// LabelNames returns all the unique label names present in the block in sorted order.
func (m *MockProm) LabelNames(ctx context.Context, startTime time.Time, endTime time.Time) ([]string, promv1.Warnings, error) {
	return []string{}, nil, nil
}

// Runtimeinfo returns the runtime info about Prometheus
func (m *MockProm) Runtimeinfo(ctx context.Context) (promv1.RuntimeinfoResult, error) {
	return promv1.RuntimeinfoResult{}, nil
}

// Metadata returns the metadata of the specified metric
func (m *MockProm) Metadata(ctx context.Context, metric string, limit string) (map[string][]promv1.Metadata, error) {
	return nil, nil
}

// Rules returns a list of alerting and recording rules that are currently loaded.
func (m *MockProm) Rules(ctx context.Context) (promv1.RulesResult, error) {
	return promv1.RulesResult{}, nil
}

// TargetsMetadata returns metadata about metrics currently scraped by the target.
func (m *MockProm) TargetsMetadata(ctx context.Context, matchTarget string, metric string, limit string) ([]promv1.MetricMetadata, error) {
	return []promv1.MetricMetadata{}, nil
}

// GenStatSummaryResponse generates a mock Public API StatSummaryResponse
// object.
func GenStatSummaryResponse(resName, resType string, resNs []string, counts *PodCounts, basicStats bool, tcpStats bool) *pb.StatSummaryResponse {
	rows := []*pb.StatTable_PodGroup_Row{}
	for _, ns := range resNs {
		statTableRow := &pb.StatTable_PodGroup_Row{
			Resource: &pb.Resource{
				Namespace: ns,
				Type:      resType,
				Name:      resName,
			},
			TimeWindow: "1m",
		}

		if basicStats {
			statTableRow.Stats = &pb.BasicStats{
				SuccessCount: 123,
				FailureCount: 0,
				LatencyMsP50: 123,
				LatencyMsP95: 123,
				LatencyMsP99: 123,
			}
		}

		if tcpStats {
			statTableRow.TcpStats = &pb.TcpStats{
				OpenConnections: 123,
				ReadBytesTotal:  123,
				WriteBytesTotal: 123,
			}
		}

		if counts != nil {
			statTableRow.MeshedPodCount = counts.MeshedPods
			statTableRow.RunningPodCount = counts.RunningPods
			statTableRow.FailedPodCount = counts.FailedPods
			statTableRow.Status = counts.Status
			statTableRow.ErrorsByPod = counts.Errors
		}

		rows = append(rows, statTableRow)
	}

	resp := &pb.StatSummaryResponse{
		Response: &pb.StatSummaryResponse_Ok_{ // https://github.com/golang/protobuf/issues/205
			Ok: &pb.StatSummaryResponse_Ok{
				StatTables: []*pb.StatTable{
					{
						Table: &pb.StatTable_PodGroup_{
							PodGroup: &pb.StatTable_PodGroup{
								Rows: rows,
							},
						},
					},
				},
			},
		},
	}

	return resp
}

// GenStatTsResponse generates a mock Public API StatSummaryResponse
// object in response to a request for trafficsplit stats.
func GenStatTsResponse(resName, resType string, resNs []string, basicStats bool, tsStats bool) *pb.StatSummaryResponse {
	leaves := map[string]string{
		"service-1": "900m",
		"service-2": "100m",
	}
	apex := "apex_name"

	rows := []*pb.StatTable_PodGroup_Row{}
	for _, ns := range resNs {
		for name, weight := range leaves {
			statTableRow := &pb.StatTable_PodGroup_Row{
				Resource: &pb.Resource{
					Namespace: ns,
					Type:      resType,
					Name:      resName,
				},
				TimeWindow: "1m",
			}

			if basicStats {
				statTableRow.Stats = &pb.BasicStats{
					SuccessCount: 123,
					FailureCount: 0,
					LatencyMsP50: 123,
					LatencyMsP95: 123,
					LatencyMsP99: 123,
				}
			}

			if tsStats {
				statTableRow.TsStats = &pb.TrafficSplitStats{
					Apex:   apex,
					Leaf:   name,
					Weight: weight,
				}
			}
			rows = append(rows, statTableRow)

		}
	}

	// sort rows before returning in order to have a consistent order for tests
	rows = sortTrafficSplitRows(rows)

	resp := &pb.StatSummaryResponse{
		Response: &pb.StatSummaryResponse_Ok_{ // https://github.com/golang/protobuf/issues/205
			Ok: &pb.StatSummaryResponse_Ok{
				StatTables: []*pb.StatTable{
					{
						Table: &pb.StatTable_PodGroup_{
							PodGroup: &pb.StatTable_PodGroup{
								Rows: rows,
							},
						},
					},
				},
			},
		},
	}
	return resp
}

type mockEdgeRow struct {
	resourceType string
	src          string
	dst          string
	srcNamespace string
	dstNamespace string
	clientID     string
	serverID     string
	msg          string
}

// a slice of edge rows to generate mock results
var emojivotoEdgeRows = []*mockEdgeRow{
	{
		resourceType: "deployment",
		src:          "web",
		dst:          "voting",
		srcNamespace: "emojivoto",
		dstNamespace: "emojivoto",
		clientID:     "web.emojivoto.serviceaccount.identity.linkerd.cluster.local",
		serverID:     "voting.emojivoto.serviceaccount.identity.linkerd.cluster.local",
		msg:          "",
	},
	{
		resourceType: "deployment",
		src:          "vote-bot",
		dst:          "web",
		srcNamespace: "emojivoto",
		dstNamespace: "emojivoto",
		clientID:     "default.emojivoto.serviceaccount.identity.linkerd.cluster.local",
		serverID:     "web.emojivoto.serviceaccount.identity.linkerd.cluster.local",
		msg:          "",
	},
	{
		resourceType: "deployment",
		src:          "web",
		dst:          "emoji",
		srcNamespace: "emojivoto",
		dstNamespace: "emojivoto",
		clientID:     "web.emojivoto.serviceaccount.identity.linkerd.cluster.local",
		serverID:     "emoji.emojivoto.serviceaccount.identity.linkerd.cluster.local",
		msg:          "",
	},
}

// a slice of edge rows to generate mock results
var linkerdEdgeRows = []*mockEdgeRow{
	{
		resourceType: "deployment",
		src:          "linkerd-controller",
		dst:          "linkerd-prometheus",
		srcNamespace: "linkerd",
		dstNamespace: "linkerd",
		clientID:     "linkerd-controller.linkerd.identity.linkerd.cluster.local",
		serverID:     "linkerd-prometheus.linkerd.identity.linkerd.cluster.local",
		msg:          "",
	},
}

// GenEdgesResponse generates a mock Public API EdgesResponse
// object.
func GenEdgesResponse(resourceType string, edgeRowNamespace string) *pb.EdgesResponse {
	edgeRows := emojivotoEdgeRows

	if edgeRowNamespace == "linkerd" {
		edgeRows = linkerdEdgeRows
	} else if edgeRowNamespace == "all" {
		// combine emojivotoEdgeRows and linkerdEdgeRows
		edgeRows = append(edgeRows, linkerdEdgeRows...)
	}

	edges := []*pb.Edge{}
	for _, row := range edgeRows {
		edge := &pb.Edge{
			Src: &pb.Resource{
				Name:      row.src,
				Namespace: row.srcNamespace,
				Type:      row.resourceType,
			},
			Dst: &pb.Resource{
				Name:      row.dst,
				Namespace: row.dstNamespace,
				Type:      row.resourceType,
			},
			ClientId:      row.clientID,
			ServerId:      row.serverID,
			NoIdentityMsg: row.msg,
		}
		edges = append(edges, edge)
	}

	// sorting to retain consistent order for tests
	edges = sortEdgeRows(edges)

	resp := &pb.EdgesResponse{
		Response: &pb.EdgesResponse_Ok_{
			Ok: &pb.EdgesResponse_Ok{
				Edges: edges,
			},
		},
	}
	return resp
}

// GenTopRoutesResponse generates a mock Public API TopRoutesResponse object.
func GenTopRoutesResponse(routes []string, counts []uint64, outbound bool, authority string) *pb.TopRoutesResponse {
	rows := []*pb.RouteTable_Row{}
	for i, route := range routes {
		row := &pb.RouteTable_Row{
			Route:     route,
			Authority: authority,
			Stats: &pb.BasicStats{
				SuccessCount: counts[i],
				FailureCount: 0,
				LatencyMsP50: 123,
				LatencyMsP95: 123,
				LatencyMsP99: 123,
			},
			TimeWindow: "1m",
		}
		if outbound {
			row.Stats.ActualSuccessCount = counts[i]
		}
		rows = append(rows, row)
	}
	defaultRow := &pb.RouteTable_Row{
		Route:     "[DEFAULT]",
		Authority: authority,
		Stats: &pb.BasicStats{
			SuccessCount: counts[len(counts)-1],
			FailureCount: 0,
			LatencyMsP50: 123,
			LatencyMsP95: 123,
			LatencyMsP99: 123,
		},
		TimeWindow: "1m",
	}
	if outbound {
		defaultRow.Stats.ActualSuccessCount = counts[len(counts)-1]
	}
	rows = append(rows, defaultRow)

	resp := &pb.TopRoutesResponse{
		Response: &pb.TopRoutesResponse_Ok_{
			Ok: &pb.TopRoutesResponse_Ok{
				Routes: []*pb.RouteTable{
					{
						Rows:     rows,
						Resource: "deploy/foobar",
					},
				},
			},
		},
	}

	return resp
}

type expectedStatRPC struct {
	err                       error
	k8sConfigs                []string    // k8s objects to seed the API
	mockPromResponse          model.Value // mock out a prometheus query response
	expectedPrometheusQueries []string    // queries we expect public-api to issue to prometheus
}

func newMockGrpcServer(exp expectedStatRPC) (*MockProm, *grpcServer, error) {
	k8sAPI, err := k8s.NewFakeAPI(exp.k8sConfigs...)
	if err != nil {
		return nil, nil, err
	}

	mockProm := &MockProm{Res: exp.mockPromResponse}
	fakeGrpcServer := newGrpcServer(
		mockProm,
		k8sAPI,
		"linkerd",
		"cluster.local",
		[]string{},
	)

	k8sAPI.Sync(nil)

	return mockProm, fakeGrpcServer, nil
}

func (exp expectedStatRPC) verifyPromQueries(mockProm *MockProm) error {
	// if exp.expectedPrometheusQueries is an empty slice we still want to check no queries were executed.
	if exp.expectedPrometheusQueries != nil {
		sort.Strings(exp.expectedPrometheusQueries)
		sort.Strings(mockProm.QueriesExecuted)

		// because reflect.DeepEqual([]string{}, nil) is false
		if len(exp.expectedPrometheusQueries) == 0 && len(mockProm.QueriesExecuted) == 0 {
			return nil
		}

		if !reflect.DeepEqual(exp.expectedPrometheusQueries, mockProm.QueriesExecuted) {
			return fmt.Errorf("Prometheus queries incorrect. \nExpected:\n%+v \nGot:\n%+v",
				exp.expectedPrometheusQueries, mockProm.QueriesExecuted)
		}
	}
	return nil
}
