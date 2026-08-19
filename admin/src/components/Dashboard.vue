<script setup>
import { ref, computed, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { adminApi } from '../api/admin'
import { setToken } from '../api/http'

const router = useRouter()

// 登录态失效：清空令牌并跳转登录页
function handleUnauth() {
  setToken('')
  router.replace('/login')
}

const dashboard = ref({ users: 0, groups: 0, messages: 0, online: 0, user_trend: [], message_trend: [] })

// 近 7 天标签（用于柱状图横轴）
const trendLabels = computed(() => {
  const labels = []
  const now = new Date()
  for (let i = 6; i >= 0; i--) {
    const d = new Date(now.getFullYear(), now.getMonth(), now.getDate() - i)
    labels.push(`${d.getMonth() + 1}/${d.getDate()}`)
  }
  return labels
})

// 生成柱状图柱子数据：data 数组 + 可选颜色
function buildBars(data, baseColor) {
  const arr = Array.isArray(data) ? data : []
  const max = Math.max(1, ...arr)
  return arr.map((v) => ({
    value: v,
    height: Math.max(2, Math.round((v / max) * 100)), // 百分比高度
    color: baseColor,
  }))
}

const userBars = computed(() => buildBars(dashboard.value.user_trend, '#1ABC9C'))
const messageBars = computed(() => buildBars(dashboard.value.message_trend, '#34D8B7'))
// 是否展示柱状图（有数据或至少已返回 7 天数组）
const showUserChart = computed(() => Array.isArray(dashboard.value.user_trend) && dashboard.value.user_trend.length > 0)
const showMessageChart = computed(() => Array.isArray(dashboard.value.message_trend) && dashboard.value.message_trend.length > 0)

async function loadDashboard() {
  try {
    dashboard.value = await adminApi.dashboard()
  } catch (e) {
    if (e.code === 401) handleUnauth()
  }
}

onMounted(loadDashboard)
</script>

<template>
  <section class="content">
    <h2>数据看板</h2>
    <div class="stat-grid">
      <div class="stat-card"><div class="stat-num">{{ dashboard.users }}</div><div class="stat-label">用户总数</div></div>
      <div class="stat-card"><div class="stat-num">{{ dashboard.groups }}</div><div class="stat-label">群组总数</div></div>
      <div class="stat-card"><div class="stat-num">{{ dashboard.messages }}</div><div class="stat-label">消息总数</div></div>
      <div class="stat-card"><div class="stat-num">{{ dashboard.online }}</div><div class="stat-label">在线用户</div></div>
    </div>

    <div class="chart-grid">
      <div class="chart-card">
        <h3>近 7 天新增用户</h3>
        <div class="bar-chart">
          <div v-for="(b, i) in userBars" :key="'u' + i" class="bar-col">
            <div class="bar-wrap">
              <div class="bar" :style="{ height: b.height + '%', background: b.color }">
                <span v-if="b.value > 0" class="bar-val">{{ b.value }}</span>
              </div>
            </div>
            <div class="bar-label">{{ trendLabels[i] }}</div>
          </div>
        </div>
        <p v-if="!showUserChart" class="chart-empty">暂无趋势数据</p>
      </div>

      <div class="chart-card">
        <h3>近 7 天消息量</h3>
        <div class="bar-chart">
          <div v-for="(b, i) in messageBars" :key="'m' + i" class="bar-col">
            <div class="bar-wrap">
              <div class="bar" :style="{ height: b.height + '%', background: b.color }">
                <span v-if="b.value > 0" class="bar-val">{{ b.value }}</span>
              </div>
            </div>
            <div class="bar-label">{{ trendLabels[i] }}</div>
          </div>
        </div>
        <p v-if="!showMessageChart" class="chart-empty">暂无趋势数据</p>
      </div>
    </div>
  </section>
</template>
