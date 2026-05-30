import { createRouter, createWebHashHistory } from 'vue-router'
import { getToken, setUnauthHandler } from '../api/http.js'

const routes = [
  { path: '/login', name: 'login', component: () => import('../views/Login.vue') },
  {
    path: '/', component: () => import('../layouts/MainLayout.vue'),
    children: [
      { path: '', name: 'dashboard', component: () => import('../views/Dashboard.vue') },
      { path: 'inbound', name: 'inbound', component: () => import('../views/Inbound.vue') },
      { path: 'outbound/subscription', name: 'subscription', component: () => import('../views/outbound/Subscription.vue') },
      { path: 'outbound/balancers', name: 'balancers', component: () => import('../views/outbound/Balancers.vue') },
      { path: 'outbound/proxies', name: 'proxies', component: () => import('../views/outbound/Proxies.vue') },
      { path: 'routing', name: 'routing', component: () => import('../views/Routing.vue') },
      { path: 'settings', name: 'settings', component: () => import('../views/Settings.vue') },
    ],
  },
]

const router = createRouter({ history: createWebHashHistory(), routes })

router.beforeEach((to) => {
  if (to.name !== 'login' && !getToken()) return { name: 'login' }
  if (to.name === 'login' && getToken()) return { name: 'dashboard' }
})

setUnauthHandler(() => router.replace({ name: 'login' }))

export default router
