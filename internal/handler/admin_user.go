package handler

import (
	"net/http"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"

	"github.com/youthlin/wenlog/internal/i18n"
	"github.com/youthlin/wenlog/internal/model"
	"github.com/youthlin/wenlog/internal/store"
)

// ListUsers 后台用户列表(admin only)。
func (h *Admin) ListUsers(c *gin.Context) {
	tr := i18n.Get(c)
	page := atoiDefault(c.Query("page"), 1)
	users, total, err := h.st.AdminListUsers(c, page, adminPageSize)
	if err != nil {
		h.serverError(c, err)
		return
	}
	data := h.base(c, tr.T("用户管理"))
	data["Users"] = users
	data["Total"] = total
	data["Page"] = page
	data["Pages"] = int((total + int64(adminPageSize) - 1) / int64(adminPageSize))
	c.HTML(http.StatusOK, "admin_users.gohtml", data)
}

// UpdateUserRole 修改用户角色(admin only)。
func (h *Admin) UpdateUserRole(c *gin.Context) {
	tr := i18n.Get(c)
	id, err := parseUintParam(c.Param("id"))
	if err != nil {
		h.notFound(c)
		return
	}
	role := c.PostForm("role")
	switch role {
	case model.RoleAdmin, model.RoleAuthor, model.RoleSubscriber:
	default:
		c.Redirect(http.StatusSeeOther, "/admin/users")
		return
	}
	if id == currentUserID(c) && role != model.RoleAdmin {
		h.renderUsersError(c, http.StatusBadRequest, tr.T("不能降低自己的管理员权限。请让其他管理员操作。"))
		return
	}
	if err := h.st.UpdateUserRole(c, id, role); err != nil {
		if errors.Is(err, store.ErrLastAdmin) {
			h.renderUsersError(c, http.StatusBadRequest, tr.T("不能将唯一的管理员改为其他角色。请先创建或指定另一个管理员。"))
			return
		}
		h.serverError(c, err)
		return
	}
	c.Redirect(http.StatusSeeOther, "/admin/users")
}

// DeleteUser 删除用户(admin only)。
func (h *Admin) DeleteUser(c *gin.Context) {
	tr := i18n.Get(c)
	id, err := parseUintParam(c.Param("id"))
	if err != nil {
		h.notFound(c)
		return
	}
	if id == currentUserID(c) {
		h.renderUsersError(c, http.StatusBadRequest, tr.T("不能在用户管理中删除自己；如需删除自己的账号，请到个人删除账号页面。"))
		return
	}
	if err := h.st.DeleteUser(c, id); err != nil {
		if errors.Is(err, store.ErrLastAdmin) {
			h.renderUsersError(c, http.StatusBadRequest, tr.T("不能删除唯一的管理员账号。请先创建或指定另一个管理员。"))
			return
		}
		h.serverError(c, err)
		return
	}
	c.Redirect(http.StatusSeeOther, "/admin/users")
}

// NewUserForm 新增用户表单(admin only)。
func (h *Admin) NewUserForm(c *gin.Context) {
	tr := i18n.Get(c)
	data := h.base(c, tr.T("新增用户"))
	c.HTML(http.StatusOK, "admin_user_form.gohtml", data)
}

// CreateUser 创建新用户(admin only)。
func (h *Admin) CreateUser(c *gin.Context) {
	tr := i18n.Get(c)
	username := strings.TrimSpace(c.PostForm("username"))
	password := c.PostForm("password")
	email := strings.TrimSpace(c.PostForm("email"))
	displayName := strings.TrimSpace(c.PostForm("display_name"))
	role := c.PostForm("role")

	if username == "" || password == "" || email == "" {
		data := h.base(c, tr.T("新增用户"))
		data["Error"] = tr.T("用户名、密码和邮箱不能为空。")
		c.HTML(http.StatusBadRequest, "admin_user_form.gohtml", data)
		return
	}
	if err := validateUsernameT(tr.T, username); err != nil {
		data := h.base(c, tr.T("新增用户"))
		data["Error"] = err.Error()
		c.HTML(http.StatusBadRequest, "admin_user_form.gohtml", data)
		return
	}
	if len(password) < 8 {
		data := h.base(c, tr.T("新增用户"))
		data["Error"] = tr.T("密码长度不能少于 8 个字符。")
		c.HTML(http.StatusBadRequest, "admin_user_form.gohtml", data)
		return
	}
	switch role {
	case model.RoleAdmin, model.RoleAuthor, model.RoleSubscriber:
	default:
		role = model.RoleSubscriber
	}

	exists, err := h.st.UserExistsByUsername(c, username, 0)
	if err != nil {
		h.serverError(c, err)
		return
	}
	if exists {
		data := h.base(c, tr.T("新增用户"))
		data["Error"] = tr.T("用户名已被占用。")
		c.HTML(http.StatusConflict, "admin_user_form.gohtml", data)
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		h.serverError(c, err)
		return
	}
	if displayName == "" {
		displayName = username
	}
	if err := h.st.CreateUser(c, username, displayName, email, string(hash), role); err != nil {
		h.serverError(c, err)
		return
	}
	c.Redirect(http.StatusSeeOther, "/admin/users")
}

// EditUserForm 编辑用户表单(admin only)。
func (h *Admin) EditUserForm(c *gin.Context) {
	tr := i18n.Get(c)
	id, err := parseUintParam(c.Param("id"))
	if err != nil {
		h.notFound(c)
		return
	}
	u, err := h.st.GetUserByID(c, id)
	if err != nil {
		h.notFound(c)
		return
	}
	data := h.base(c, tr.T("编辑用户"))
	data["EditUser"] = u
	c.HTML(http.StatusOK, "admin_user_form.gohtml", data)
}

// UpdateUser 更新用户信息(admin only, 不含密码)。
func (h *Admin) UpdateUser(c *gin.Context) {
	tr := i18n.Get(c)
	id, err := parseUintParam(c.Param("id"))
	if err != nil {
		h.notFound(c)
		return
	}
	username := strings.TrimSpace(c.PostForm("username"))
	email := strings.TrimSpace(c.PostForm("email"))
	displayName := strings.TrimSpace(c.PostForm("display_name"))
	role := c.PostForm("role")

	if username == "" || email == "" {
		u, _ := h.st.GetUserByID(c, id)
		data := h.base(c, tr.T("编辑用户"))
		data["EditUser"] = u
		data["Error"] = tr.T("用户名和邮箱不能为空。")
		c.HTML(http.StatusBadRequest, "admin_user_form.gohtml", data)
		return
	}
	if err := validateUsernameT(tr.T, username); err != nil {
		u, _ := h.st.GetUserByID(c, id)
		data := h.base(c, tr.T("编辑用户"))
		data["EditUser"] = u
		data["Error"] = err.Error()
		c.HTML(http.StatusBadRequest, "admin_user_form.gohtml", data)
		return
	}
	switch role {
	case model.RoleAdmin, model.RoleAuthor, model.RoleSubscriber:
	default:
		role = model.RoleSubscriber
	}
	if id == currentUserID(c) && role != model.RoleAdmin {
		u, _ := h.st.GetUserByID(c, id)
		data := h.base(c, tr.T("编辑用户"))
		data["EditUser"] = u
		data["Error"] = tr.T("不能降低自己的管理员权限。请让其他管理员操作。")
		c.HTML(http.StatusBadRequest, "admin_user_form.gohtml", data)
		return
	}

	exists, err := h.st.UserExistsByUsername(c, username, id)
	if err != nil {
		h.serverError(c, err)
		return
	}
	if exists {
		u, _ := h.st.GetUserByID(c, id)
		data := h.base(c, tr.T("编辑用户"))
		data["EditUser"] = u
		data["Error"] = tr.T("用户名已被占用。")
		c.HTML(http.StatusConflict, "admin_user_form.gohtml", data)
		return
	}

	if displayName == "" {
		displayName = username
	}
	u, err := h.st.GetUserByID(c, id)
	if err != nil {
		h.notFound(c)
		return
	}
	if err := h.st.UpdateUserProfile(c, id, username, displayName, email, u.Website); err != nil {
		h.serverError(c, err)
		return
	}
	if err := h.st.UpdateUserRole(c, id, role); err != nil {
		if errors.Is(err, store.ErrLastAdmin) {
			u, _ := h.st.GetUserByID(c, id)
			data := h.base(c, tr.T("编辑用户"))
			data["EditUser"] = u
			data["Error"] = tr.T("不能将唯一的管理员改为其他角色。请先创建或指定另一个管理员。")
			c.HTML(http.StatusBadRequest, "admin_user_form.gohtml", data)
			return
		}
		h.serverError(c, err)
		return
	}
	c.Redirect(http.StatusSeeOther, "/admin/users")
}

func (h *Admin) renderUsersError(c *gin.Context, status int, msg string) {
	tr := i18n.Get(c)
	page := atoiDefault(c.Query("page"), 1)
	users, total, err := h.st.AdminListUsers(c, page, adminPageSize)
	if err != nil {
		h.serverError(c, err)
		return
	}
	data := h.base(c, tr.T("用户管理"))
	data["Users"] = users
	data["Total"] = total
	data["Page"] = page
	data["Pages"] = int((total + int64(adminPageSize) - 1) / int64(adminPageSize))
	data["Error"] = msg
	c.HTML(status, "admin_users.gohtml", data)
}
