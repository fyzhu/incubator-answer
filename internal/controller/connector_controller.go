package controller

import (
	"fmt"
	"net/http"

	"github.com/answerdev/answer/internal/base/handler"
	"github.com/answerdev/answer/internal/base/middleware"
	"github.com/answerdev/answer/internal/schema"
	"github.com/answerdev/answer/internal/service/export"
	"github.com/answerdev/answer/internal/service/siteinfo_common"
	"github.com/answerdev/answer/internal/service/user_external_login"
	"github.com/answerdev/answer/plugin"
	"github.com/gin-gonic/gin"
	"github.com/segmentfault/pacman/log"
)

const (
	commonRouterPrefix            = "/answer/api/v1"
	ConnectorLoginRouterPrefix    = "/connector/login/"
	ConnectorRedirectRouterPrefix = "/connector/redirect/"
)

// ConnectorController comment controller
type ConnectorController struct {
	siteInfoService     *siteinfo_common.SiteInfoCommonService
	userExternalService *user_external_login.UserExternalLoginService
	emailService        *export.EmailService
}

// NewConnectorController new controller
func NewConnectorController(
	siteInfoService *siteinfo_common.SiteInfoCommonService,
	emailService *export.EmailService,
	userExternalService *user_external_login.UserExternalLoginService,
) *ConnectorController {
	return &ConnectorController{
		siteInfoService:     siteInfoService,
		userExternalService: userExternalService,
		emailService:        emailService,
	}
}

func (cc *ConnectorController) ConnectorLogin(connector plugin.Connector) (fn func(ctx *gin.Context)) {
	return func(ctx *gin.Context) {
		general, err := cc.siteInfoService.GetSiteGeneral(ctx)
		if err != nil {
			log.Error(err)
			ctx.Redirect(http.StatusFound, "/50x")
			return
		}

		receiverURL := fmt.Sprintf("%s%s%s%s", general.SiteUrl,
			commonRouterPrefix, ConnectorRedirectRouterPrefix, connector.ConnectorSlugName())
		redirectURL := connector.ConnectorSender(ctx, receiverURL)
		if len(redirectURL) > 0 {
			ctx.Redirect(http.StatusFound, redirectURL)
		}
		return
	}
}

func (cc *ConnectorController) ConnectorRedirect(connector plugin.Connector) (fn func(ctx *gin.Context)) {
	return func(ctx *gin.Context) {
		siteGeneral, err := cc.siteInfoService.GetSiteGeneral(ctx)
		if err != nil {
			log.Errorf("get site info failed: %v", err)
			ctx.Redirect(http.StatusFound, "/50x")
			return
		}
		userInfo, err := connector.ConnectorReceiver(ctx)
		if err != nil {
			log.Errorf("connector received failed: %v", err)
			ctx.Redirect(http.StatusFound, "/50x")
			return
		}
		u := &schema.ExternalLoginUserInfoCache{
			Provider:   connector.ConnectorSlugName(),
			ExternalID: userInfo.ExternalID,
			Name:       userInfo.Name,
			Email:      userInfo.Email,
			MetaInfo:   userInfo.MetaInfo,
		}
		resp, err := cc.userExternalService.ExternalLogin(ctx, u)
		if err != nil {
			log.Errorf("external login failed: %v", err)
			ctx.Redirect(http.StatusFound, "/50x")
			return
		}
		if len(resp.AccessToken) > 0 {
			ctx.Redirect(http.StatusFound, fmt.Sprintf("%s/users/oauth?access_token=%s",
				siteGeneral.SiteUrl, resp.AccessToken))
		} else {
			ctx.Redirect(http.StatusFound, fmt.Sprintf("%s/users/confirm-email?binding_key=%s",
				siteGeneral.SiteUrl, resp.BindingKey))
		}
	}
}

// ConnectorsInfo get all enabled connectors
// @Summary get all enabled connectors
// @Description get all enabled connectors
// @Tags PluginConnector
// @Security ApiKeyAuth
// @Produce  json
// @Success 200 {object} handler.RespBody{data=[]schema.ConnectorInfoResp}
// @Router /answer/api/v1/connector/info [get]
func (cc *ConnectorController) ConnectorsInfo(ctx *gin.Context) {
	general, err := cc.siteInfoService.GetSiteGeneral(ctx)
	if err != nil {
		handler.HandleResponse(ctx, err, nil)
		return
	}

	resp := make([]*schema.ConnectorInfoResp, 0)
	_ = plugin.CallConnector(func(fn plugin.Connector) error {
		resp = append(resp, &schema.ConnectorInfoResp{
			Name: fn.ConnectorName()(ctx),
			Icon: fn.ConnectorLogoSVG(),
			Link: fmt.Sprintf("%s%s%s%s", general.SiteUrl,
				commonRouterPrefix, ConnectorLoginRouterPrefix, fn.ConnectorSlugName()),
		})
		return nil
	})
	handler.HandleResponse(ctx, nil, resp)
}

// ExternalLoginBindingUserSendEmail external login binding user send email
// @Summary external login binding user send email
// @Description external login binding user send email
// @Tags PluginConnector
// @Accept json
// @Produce json
// @Param data body schema.ExternalLoginBindingUserSendEmailReq  true "external login binding user send email"
// @Success 200 {object} handler.RespBody{data=schema.ExternalLoginBindingUserSendEmailResp}
// @Router /answer/api/v1/connector/binding/email [post]
func (cc *ConnectorController) ExternalLoginBindingUserSendEmail(ctx *gin.Context) {
	req := &schema.ExternalLoginBindingUserSendEmailReq{}
	if handler.BindAndCheck(ctx, req) {
		return
	}

	resp, err := cc.userExternalService.ExternalLoginBindingUserSendEmail(ctx, req)
	handler.HandleResponse(ctx, err, resp)
}

// ConnectorsUserInfo get all connectors info about user
// @Summary get all connectors info about user
// @Description get all connectors info about user
// @Tags PluginConnector
// @Security ApiKeyAuth
// @Produce json
// @Success 200 {object} handler.RespBody{data=[]schema.ConnectorUserInfoResp}
// @Router /answer/api/v1/connector/user/info [get]
func (cc *ConnectorController) ConnectorsUserInfo(ctx *gin.Context) {
	general, err := cc.siteInfoService.GetSiteGeneral(ctx)
	if err != nil {
		handler.HandleResponse(ctx, err, nil)
		return
	}

	userID := middleware.GetLoginUserIDFromContext(ctx)

	userInfoList, err := cc.userExternalService.GetExternalLoginUserInfoList(ctx, userID)
	if err != nil {
		handler.HandleResponse(ctx, err, nil)
		return
	}
	userExternalLoginMapping := make(map[string]string)
	for _, userInfo := range userInfoList {
		userExternalLoginMapping[userInfo.Provider] = userInfo.ExternalID
	}

	resp := make([]*schema.ConnectorUserInfoResp, 0)
	_ = plugin.CallConnector(func(fn plugin.Connector) error {
		externalID := userExternalLoginMapping[fn.ConnectorSlugName()]
		resp = append(resp, &schema.ConnectorUserInfoResp{
			Name: fn.ConnectorName()(ctx),
			Icon: fn.ConnectorLogoSVG(),
			Link: fmt.Sprintf("%s%s%s%s", general.SiteUrl,
				commonRouterPrefix, ConnectorLoginRouterPrefix, fn.ConnectorSlugName()),
			Binding:    len(externalID) > 0,
			ExternalID: externalID,
		})
		return nil
	})
	handler.HandleResponse(ctx, nil, resp)
}

// ExternalLoginUnbinding unbind external user login
// @Summary unbind external user login
// @Description unbind external user login
// @Tags PluginConnector
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Param data body schema.ExternalLoginUnbindingReq true "ExternalLoginUnbindingReq"
// @Success 200 {object} handler.RespBody{}
// @Router /answer/api/v1/connector/user/unbinding [delete]
func (cc *ConnectorController) ExternalLoginUnbinding(ctx *gin.Context) {
	req := &schema.ExternalLoginUnbindingReq{}
	if handler.BindAndCheck(ctx, req) {
		return
	}

	req.UserID = middleware.GetLoginUserIDFromContext(ctx)

	err := cc.userExternalService.ExternalLoginUnbinding(ctx, req)
	handler.HandleResponse(ctx, err, nil)
}
