import { createRouter, createWebHistory } from 'vue-router'
import { isAuthed } from '../api/http'
import AdminLayout from '../layouts/AdminLayout.vue'
import LoginView from '../components/LoginView.vue'
import Dashboard from '../components/Dashboard.vue'
import UserManage from '../components/UserManage.vue'
import GroupManage from '../components/GroupManage.vue'
import VersionManage from '../components/VersionManage.vue'

const routes = [
  {
    path: '/login',
    name: 'login',
    component: LoginView,
    meta: { public: true },
  },
  {
    path: '/',
    component: AdminLayout,
    children: [
      { path: '', redirect: '/dashboard' },
      { path: 'dashboard', name: 'dashboard', component: Dashboard },
      { path: 'users', name: 'users', component: UserManage },
      { path: 'groups', name: 'groups', component: GroupManage },
      { path: 'versions', name: 'versions', component: VersionManage },
    ],
  },
  { path: '/:pathMatch(.*)*', redirect: '/dashboard' },
]

const router = createRouter({
  history: createWebHistory(),
  routes,
})

// 全局登录守卫：非公开页面未登录跳转 /login；已登录访问 /login 跳转 /dashboard
router.beforeEach((to) => {
  const authed = isAuthed()
  if (to.meta.public) {
    if (authed && to.name === 'login') return { path: '/dashboard' }
    return true
  }
  if (!authed) return { path: '/login' }
  return true
})

export default router
