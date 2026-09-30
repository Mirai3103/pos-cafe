package catalog

import (
	"context"
	"strconv"

	"github.com/labstack/echo/v4"
)

// handleGetSellableMenu godoc
//
//	@Summary		Lấy thực đơn bán hàng (Sellable Menu)
//	@Description	Trả về projection thực đơn chỉ bao gồm các món và kích thước còn hàng, sẵn sàng để bán. Yêu cầu quyền catalog.view_prices.
//	@Tags			Catalog
//	@Produce		json
//	@Security		BearerAuth
//	@Success		200	{object}	response.APIResponse{data=SellableMenuResponse}
//	@Failure		400	{object}	response.APIResponse
//	@Failure		401	{object}	response.APIResponse
//	@Failure		403	{object}	response.APIResponse
//	@Failure		404	{object}	response.APIResponse
//	@Failure		409	{object}	response.APIResponse
//	@Failure		500	{object}	response.APIResponse
//	@Router			/catalog/menu/sellable [get]
func (s *Slices) handleGetSellableMenu(c echo.Context) error {
	return sendRead(c, s.SellableMenu.Handle)
}

// handleGetManagementMenu godoc
//
//	@Summary		Lấy thực đơn quản lý (Management Menu)
//	@Description	Trả về toàn bộ danh mục, món, kích thước (bao gồm cả món đã ngừng bán hoặc hết hàng) phục vụ quản trị. Yêu cầu quyền catalog.view_prices.
//	@Tags			Catalog
//	@Produce		json
//	@Security		BearerAuth
//	@Success		200	{object}	response.APIResponse{data=ManagementMenuResponse}
//	@Failure		400	{object}	response.APIResponse
//	@Failure		401	{object}	response.APIResponse
//	@Failure		403	{object}	response.APIResponse
//	@Failure		404	{object}	response.APIResponse
//	@Failure		409	{object}	response.APIResponse
//	@Failure		500	{object}	response.APIResponse
//	@Router			/catalog/menu/manage [get]
func (s *Slices) handleGetManagementMenu(c echo.Context) error {
	return sendRead(c, s.ManagementMenu.Handle)
}

// handleGetAvailabilityMenu godoc
//
//	@Summary		Lấy thực đơn trạng thái khả dụng (Availability Menu)
//	@Description	Trả về projection nhẹ danh mục, món, size, tùy chọn và cờ khả dụng để thu ngân/pha chế bật tắt nhanh. Yêu cầu quyền catalog.manage_availability.
//	@Tags			Catalog
//	@Produce		json
//	@Security		BearerAuth
//	@Success		200	{object}	response.APIResponse{data=AvailabilityMenuResponse}
//	@Failure		400	{object}	response.APIResponse
//	@Failure		401	{object}	response.APIResponse
//	@Failure		403	{object}	response.APIResponse
//	@Failure		404	{object}	response.APIResponse
//	@Failure		409	{object}	response.APIResponse
//	@Failure		500	{object}	response.APIResponse
//	@Router			/catalog/menu/availability [get]
func (s *Slices) handleGetAvailabilityMenu(c echo.Context) error {
	return sendRead(c, s.AvailabilityMenu.Handle)
}

// handleGetModifierGroups godoc
//
//	@Summary		Danh sách nhóm tùy chọn / topping quản lý
//	@Description	Trả về danh sách tất cả các nhóm modifier cùng các tùy chọn và cấu hình mặc định. Yêu cầu quyền catalog.view_prices.
//	@Tags			Catalog
//	@Produce		json
//	@Security		BearerAuth
//	@Success		200	{object}	response.APIResponse{data=[]ManagementModifierGroupResponse}
//	@Failure		400	{object}	response.APIResponse
//	@Failure		401	{object}	response.APIResponse
//	@Failure		403	{object}	response.APIResponse
//	@Failure		404	{object}	response.APIResponse
//	@Failure		409	{object}	response.APIResponse
//	@Failure		500	{object}	response.APIResponse
//	@Router			/catalog/modifier-groups [get]
func (s *Slices) handleGetModifierGroups(c echo.Context) error {
	return sendRead(c, s.ModifierGroups.Handle)
}

// handleGetAuditEvents godoc
//
//	@Summary		Danh sách nhật ký kiểm toán catalog
//	@Description	Trả về danh sách sự kiện audit của catalog theo thứ tự mới nhất trước. Yêu cầu quyền audit.inspect.
//	@Tags			Catalog
//	@Produce		json
//	@Security		BearerAuth
//	@Param			limit	query		int	false	"Số lượng bản ghi tối đa (mặc định 50, tối đa 100)"
//	@Success		200		{object}	response.APIResponse{data=[]AuditEventResponse}
//	@Failure		400		{object}	response.APIResponse
//	@Failure		401		{object}	response.APIResponse
//	@Failure		403		{object}	response.APIResponse
//	@Failure		404		{object}	response.APIResponse
//	@Failure		409		{object}	response.APIResponse
//	@Failure		500		{object}	response.APIResponse
//	@Router			/catalog/audit-events [get]
func (s *Slices) handleGetAuditEvents(c echo.Context) error {
	limit := auditLimit(c.QueryParam("limit"))
	return sendRead(c, func(ctx context.Context, actor Actor) ([]AuditEventResponse, error) {
		return s.AuditEvents.Handle(ctx, actor, limit)
	})
}

// auditLimit parses the optional limit query parameter. A missing, malformed,
// or non-positive value falls back to 50; the handler caps the upper bound.
func auditLimit(raw string) int32 {
	if raw != "" {
		if parsed, err := strconv.ParseInt(raw, 10, 32); err == nil && parsed > 0 {
			return int32(parsed)
		}
	}
	return 50
}
