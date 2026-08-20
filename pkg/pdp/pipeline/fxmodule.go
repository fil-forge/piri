package pipeline

import (
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"go.uber.org/fx"

	aggregatorpolicy "github.com/fil-forge/piri/pkg/pdp/aggregation/aggregator/policy"
	"github.com/fil-forge/piri/pkg/pdp/aggregation/commp"
	"github.com/fil-forge/piri/pkg/pdp/aggregation/manager"
	aggtypes "github.com/fil-forge/piri/pkg/pdp/aggregation/types"
	"github.com/fil-forge/piri/pkg/pdp/service"
)

// Module wires the aggregation pipeline tasks into the curiopdp harmonytask
// engine (via the curio_harmonytasks group) and provides the pipeline's
// public seams: commp.Calculator for /blob/accept and RootSubmitter for the
// aggregation fold.
var Module = fx.Module("pdp/pipeline",
	fx.Provide(
		aggtypes.NewStore,
		aggregatorpolicy.New,
		manager.NewConfigProvider,
		manager.NewPieceAccepter,

		NewCommPTask,
		NewAggregateTask,
		NewAddRootsTask,
		NewRemoveSweepTask,

		fx.Annotate(NewSubmissionManager, fx.As(new(RootSubmitter))),
		fx.Annotate(NewEntry, fx.As(new(commp.Calculator))),
		func(s *service.PDPService) RemovalSweeper { return s },

		fx.Annotate(asTask[*CommPTask], fx.ResultTags(taskGroup)),
		fx.Annotate(asTask[*AggregateTask], fx.ResultTags(taskGroup)),
		fx.Annotate(asTask[*AddRootsTask], fx.ResultTags(taskGroup)),
		fx.Annotate(asTask[*RemoveSweepTask], fx.ResultTags(taskGroup)),
	),
)

func asTask[T harmonytask.TaskInterface](t T) harmonytask.TaskInterface { return t }
