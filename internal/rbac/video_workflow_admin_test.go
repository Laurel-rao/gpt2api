package rbac

import "testing"

func menuContainsPath(items []Menu, path string) bool {
	for _, item := range items {
		if item.Path == path || menuContainsPath(item.Children, path) {
			return true
		}
	}
	return false
}

func TestVideoWorkflowIsAdminOnly(t *testing.T) {
	const path = "/personal/video-workflows"
	if Has(RoleUser, PermSelfVideoWorkflow) {
		t.Fatal("普通用户不应持有视频工作流权限")
	}
	if !Has(RoleAdmin, PermSelfVideoWorkflow) {
		t.Fatal("管理员应持有视频工作流权限")
	}
	if menuContainsPath(MenuForRole(RoleUser), path) {
		t.Fatal("普通用户菜单不应显示视频工作流")
	}
	if !menuContainsPath(MenuForRole(RoleAdmin), path) {
		t.Fatal("管理员菜单应显示视频工作流")
	}
}
