<script setup>
import { ref, computed, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { adminApi } from '../api/admin'
import { setToken } from '../api/http'
import { useUi } from '../composables/useUi'

const router = useRouter()
const { openConfirm, showToast } = useUi()

// 登录态失效：清空令牌并跳转登录页
function handleUnauth() {
  setToken('')
  router.replace('/login')
}

const users = ref([])
const usersTotal = ref(0)
const userPage = ref(1)
const userKeyword = ref('')
const userStatus = ref(0) // 0=全部 1=正常 2=已禁用
const pageSize = 10

const userPageCount = computed(() => Math.max(1, Math.ceil(usersTotal.value / pageSize)))

function fmtTime(unixSec) {
  if (!unixSec) return '-'
  const d = new Date(unixSec * 1000)
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')} ${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`
}

async function loadUsers() {
  try {
    const offset = (userPage.value - 1) * pageSize
    const res = await adminApi.listUsers(offset, pageSize, userKeyword.value, userStatus.value)
    users.value = res.list || []
    usersTotal.value = res.total || 0
  } catch (e) {
    if (e.code === 401) handleUnauth()
  }
}

// 用户搜索：重置到第 1 页并重新加载
function searchUsers() {
  userPage.value = 1
  loadUsers()
}
// 用户状态下拉：切换后重新搜索
function changeUserStatus() {
  searchUsers()
}
// 清空用户搜索
function clearUserSearch() {
  if (!userKeyword.value) return
  userKeyword.value = ''
  userPage.value = 1
  loadUsers()
}

function disableUser(uid) {
  openConfirm({
    title: '禁用用户',
    message: '确定要禁用该用户吗？禁用后将无法登录，且不能撤销。',
    confirmText: '确认禁用',
    danger: true,
    onConfirm: async () => {
      try {
        await adminApi.disableUser(uid)
        loadUsers()
        showToast('用户已禁用，其在线连接将被断开', 'ok')
      } catch (e) {
        showToast(e.message || '操作失败', 'error')
      }
    },
  })
}

function enableUser(uid) {
  openConfirm({
    title: '启用用户',
    message: '确定要启用该用户吗？启用后可正常登录使用。',
    confirmText: '确认启用',
    danger: false,
    onConfirm: async () => {
      try {
        await adminApi.enableUser(uid)
        loadUsers()
        showToast('用户已启用', 'ok')
      } catch (e) {
        showToast(e.message || '操作失败', 'error')
      }
    },
  })
}

onMounted(loadUsers)
</script>

<template>
  <section class="content">
    <h2>用户管理 <span class="count">共 {{ usersTotal }} 人</span></h2>
    <div class="toolbar">
      <div class="search-box">
        <select v-model="userStatus" class="search-select" @change="changeUserStatus">
          <option :value="0">全部状态</option>
          <option :value="1">正常</option>
          <option :value="2">已禁用</option>
        </select>
        <input v-model="userKeyword" class="search-input" placeholder="搜索昵称 / 账号"
          @keyup.enter="searchUsers" />
        <button v-if="userKeyword" class="link-btn" @click="clearUserSearch">清空</button>
        <button class="btn" @click="searchUsers">搜索</button>
      </div>
    </div>
    <table class="table">
      <thead><tr><th>UID</th><th>账号</th><th>昵称</th><th>状态</th><th>注册时间</th><th>操作</th></tr></thead>
      <tbody>
        <tr v-for="u in users" :key="u.uid">
          <td>{{ u.uid }}</td>
          <td>{{ u.account }}</td>
          <td>{{ u.nickname }}</td>
          <td><span :class="['badge', u.disabled === 1 ? 'off' : 'ok']">{{ u.disabled === 1 ? '已禁用' : '正常' }}</span></td>
          <td>{{ fmtTime(u.created_at) }}</td>
          <td>
            <button v-if="u.disabled === 1" class="link-btn" @click="enableUser(u.uid)">启用</button>
            <button v-else class="link-btn danger" @click="disableUser(u.uid)">禁用</button>
          </td>
        </tr>
        <tr v-if="!users.length"><td colspan="6" class="empty">暂无数据</td></tr>
      </tbody>
    </table>
    <div class="pager">
      <button :disabled="userPage <= 1" @click="userPage--; loadUsers()">上一页</button>
      <span>{{ userPage }} / {{ userPageCount }}</span>
      <button :disabled="userPage >= userPageCount" @click="userPage++; loadUsers()">下一页</button>
    </div>
  </section>
</template>
