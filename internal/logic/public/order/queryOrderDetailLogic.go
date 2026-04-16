package order

import (
	"context"
	"strings"

	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

type QueryOrderDetailLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Get order
func NewQueryOrderDetailLogic(ctx context.Context, svcCtx *svc.ServiceContext) *QueryOrderDetailLogic {
	return &QueryOrderDetailLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *QueryOrderDetailLogic) QueryOrderDetail(req *types.QueryOrderDetailRequest) (resp *types.OrderDetail, err error) {
	// Sanitize order number: strip quotes and truncate at '?' to remove URL artifacts
	orderNo := strings.ReplaceAll(req.OrderNo, "\"", "")
	if idx := strings.Index(orderNo, "?"); idx != -1 {
		orderNo = orderNo[:idx]
	}
	orderInfo, err := l.svcCtx.OrderModel.FindOneDetailsByOrderNo(l.ctx, orderNo)
	if err != nil {
		l.Errorw("[QueryOrderDetail] Database query error", logger.Field("error", err.Error()), logger.Field("order_no", req.OrderNo))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find order error: %v", err.Error())
	}
	resp = &types.OrderDetail{}
	tool.DeepCopy(resp, orderInfo)
	// Prevent commission amount leakage
	resp.Commission = 0
	return
}
