<template>
  <main class="login-shell">
    <section class="login-stage">
      <span
        v-for="index in 6"
        :key="index"
        class="float-square"
        :style="{ '--i': index - 1 }"
      />

      <div class="glass-card">
        <form class="login-form" @submit.prevent="submit">
          <div class="brand">
            <LogoIcon class="brand-icon" />
            <div>
              <div class="brand-title">frp</div>
              <div class="brand-subtitle">Server Console</div>
            </div>
          </div>

          <h1>LOGIN</h1>

          <el-alert
            v-if="error"
            :title="error"
            type="error"
            show-icon
            :closable="false"
          />

          <label class="input-box">
            <input v-model="username" autocomplete="username" required autofocus />
            <span>Username</span>
            <span class="input-icon">◎</span>
          </label>

          <label class="input-box">
            <input
              v-model="password"
              :type="showPassword ? 'text' : 'password'"
              autocomplete="current-password"
              required
            />
            <span>Password</span>
            <button
              type="button"
              class="password-toggle"
              :aria-label="showPassword ? 'Hide password' : 'Show password'"
              @click="showPassword = !showPassword"
            >
              {{ showPassword ? '◉' : '○' }}
            </button>
            <span class="input-icon">◆</span>
          </label>

          <label class="remember-row">
            <input v-model="remember" type="checkbox" />
            <span>Remember</span>
          </label>

          <button class="login-button" type="submit" :disabled="loading">
            {{ loading ? 'Logging in' : 'Log in' }}
          </button>
        </form>
      </div>
    </section>
  </main>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { getSession, type SessionInfo } from '../api/session'
import { clearBasicAuth, HTTPError, setBasicAuth } from '../api/http'
import LogoIcon from '../assets/icons/logo.svg?component'

const emit = defineEmits<{
  loggedIn: [session: SessionInfo]
}>()

const username = ref('')
const password = ref('')
const loading = ref(false)
const error = ref('')
const showPassword = ref(false)
const remember = ref(true)

async function submit() {
  error.value = ''
  loading.value = true
  setBasicAuth(username.value, password.value)
  try {
    const session = await getSession()
    emit('loggedIn', session)
  } catch (err) {
    clearBasicAuth()
    if (err instanceof HTTPError && err.status === 401) {
      error.value = 'Invalid username or password'
    } else {
      error.value = 'Login failed'
    }
  } finally {
    loading.value = false
  }
}
</script>

<style scoped>
.login-shell {
  min-height: 100vh;
  min-height: 100dvh;
  display: grid;
  place-items: center;
  overflow: hidden;
  padding: 28px;
  background: linear-gradient(-45deg, #ee7752, #e73c7e, #23a6d5, #23d5ab);
  background-size: 400% 400%;
  animation: login-gradient 10s ease infinite;
}

@keyframes login-gradient {
  0% {
    background-position: 0% 50%;
  }

  50% {
    background-position: 100% 50%;
  }

  100% {
    background-position: 0% 50%;
  }
}

.login-stage {
  position: relative;
  width: min(420px, 100%);
}

.float-square {
  position: absolute;
  display: block;
  background: rgba(255, 255, 255, 0.12);
  border: 1px solid rgba(255, 255, 255, 0.22);
  border-radius: 15px;
  box-shadow: 0 25px 45px rgba(0, 0, 0, 0.12);
  backdrop-filter: blur(5px);
  animation: float-square 10s linear infinite;
  animation-delay: calc(-1s * var(--i));
}

.float-square:nth-child(1) {
  width: 100px;
  height: 100px;
  top: -28px;
  right: -42px;
}

.float-square:nth-child(2) {
  width: 150px;
  height: 150px;
  top: 116px;
  left: -132px;
  z-index: 2;
}

.float-square:nth-child(3) {
  width: 62px;
  height: 62px;
  right: -42px;
  bottom: 92px;
  z-index: 2;
}

.float-square:nth-child(4) {
  width: 54px;
  height: 54px;
  bottom: 34px;
  left: -88px;
}

.float-square:nth-child(5) {
  width: 52px;
  height: 52px;
  top: -16px;
  left: -22px;
}

.float-square:nth-child(6) {
  width: 86px;
  height: 86px;
  top: 178px;
  right: -148px;
  z-index: 2;
}

@keyframes float-square {
  0%,
  100% {
    transform: translateY(-20px);
  }

  50% {
    transform: translateY(20px);
  }
}

.glass-card {
  position: relative;
  min-height: 430px;
  padding: 48px;
  border-radius: 10px;
  border: 1px solid rgba(255, 255, 255, 0.2);
  background: rgba(255, 255, 255, 0.12);
  backdrop-filter: blur(8px);
  box-shadow: 0 25px 45px rgba(0, 0, 0, 0.2);
}

.glass-card::after {
  content: '';
  position: absolute;
  inset: 6px;
  pointer-events: none;
  border-radius: 6px;
  border-top: 1px solid rgba(255, 255, 255, 0.28);
}

.brand {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 24px;
  color: #ffffff;
}

.brand-icon {
  width: 34px;
  height: 34px;
}

.brand-title {
  font-size: 22px;
  font-weight: 700;
  line-height: 1;
}

.brand-subtitle {
  margin-top: 4px;
  color: rgba(255, 255, 255, 0.68);
  font-size: 12px;
}

.login-form {
  position: relative;
  z-index: 3;
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.login-form h1 {
  margin: 0;
  color: #ffffff;
  font-size: 24px;
  letter-spacing: 2px;
}

.input-box {
  position: relative;
  display: block;
}

.input-box input {
  width: 100%;
  height: 42px;
  outline: none;
  border: 1px solid rgba(255, 255, 255, 0.24);
  border-radius: 15px;
  padding: 9px 42px;
  background: rgba(255, 255, 255, 0.2);
  color: #ffffff;
  font-size: 16px;
  box-shadow: 0 5px 15px rgba(0, 0, 0, 0.06);
}

.input-box input:focus {
  border-color: rgba(255, 255, 255, 0.6);
}

.input-box > span:first-of-type {
  position: absolute;
  left: 32px;
  top: 10px;
  color: #ffffff;
  transition:
    transform 0.2s ease,
    font-size 0.2s ease,
    opacity 0.2s ease;
  pointer-events: none;
}

.input-box input:focus ~ span:first-of-type,
.input-box input:valid ~ span:first-of-type {
  transform: translate(-28px, -26px);
  font-size: 12px;
}

.input-icon {
  position: absolute;
  left: 14px;
  top: 11px;
  color: rgba(255, 255, 255, 0.78);
  font-size: 14px;
}

.password-toggle {
  position: absolute;
  right: 12px;
  top: 8px;
  width: 26px;
  height: 26px;
  border: 0;
  border-radius: 50%;
  background: transparent;
  color: #ffffff;
  cursor: pointer;
}

.remember-row {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  color: #ffffff;
  font-size: 14px;
  cursor: pointer;
  user-select: none;
}

.login-button {
  width: 112px;
  height: 38px;
  border: 0;
  border-radius: 15px;
  background: #ffffff;
  color: #111111;
  font-weight: 700;
  letter-spacing: 1px;
  cursor: pointer;
  transition:
    color 0.2s ease,
    background 0.2s ease;
}

.login-button:hover:not(:disabled) {
  color: #ffffff;
  background: linear-gradient(
    115deg,
    rgba(0, 0, 0, 0.1),
    rgba(255, 255, 255, 0.25)
  );
}

.login-button:disabled {
  cursor: default;
  opacity: 0.72;
}

:deep(.el-alert) {
  border: 1px solid rgba(245, 108, 108, 0.3);
  background: rgba(245, 108, 108, 0.18);
}

:deep(.el-alert__title),
:deep(.el-alert__icon) {
  color: #ffffff;
}

@media (max-width: 760px) {
  .login-stage {
    width: min(360px, 100%);
  }

  .glass-card {
    padding: 34px 28px;
  }

  .float-square:nth-child(2),
  .float-square:nth-child(4),
  .float-square:nth-child(6) {
    display: none;
  }
}
</style>
