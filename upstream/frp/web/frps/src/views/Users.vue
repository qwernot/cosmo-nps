<template>
  <div class="users-page">
    <div class="page-header">
      <div>
        <h1 class="page-title">Users</h1>
        <p class="page-subtitle">Manage frpc identities and assigned port pools.</p>
      </div>
      <el-button type="primary" @click="openCreate">New User</el-button>
    </div>

    <el-card>
      <el-table v-loading="loading" :data="users" style="width: 100%">
        <el-table-column prop="name" label="User" min-width="160" />
        <el-table-column prop="role" label="Role" width="110">
          <template #default="{ row }">
            <el-tag :type="row.role === 'admin' ? 'warning' : 'info'">
              {{ row.role || 'user' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="Status" width="110">
          <template #default="{ row }">
            <el-tag :type="row.enabled ? 'success' : 'danger'">
              {{ row.enabled ? 'Enabled' : 'Disabled' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="Ports" min-width="220">
          <template #default="{ row }">
            {{ formatPorts(row.allowPorts) || '-' }}
          </template>
        </el-table-column>
        <el-table-column prop="maxPorts" label="Max" width="90" />
        <el-table-column label="Auth" width="150">
          <template #default="{ row }">
            <el-tag v-if="row.hasPassword" size="small">web</el-tag>
            <el-tag v-if="row.hasToken" size="small" class="token-tag">frpc</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="Actions" width="170" align="right">
          <template #default="{ row }">
            <el-button size="small" @click="openEdit(row)">Edit</el-button>
            <el-button size="small" type="danger" @click="remove(row.name)">Delete</el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-card>

    <el-dialog v-model="dialogVisible" :title="editing ? 'Edit User' : 'New User'" width="520px">
      <el-form label-position="top">
        <el-form-item label="User">
          <el-input v-model="form.name" :disabled="editing" />
        </el-form-item>
        <el-form-item label="Role">
          <el-select v-model="form.role" style="width: 100%">
            <el-option label="User" value="user" />
            <el-option label="Admin" value="admin" />
          </el-select>
        </el-form-item>
        <el-form-item label="Status">
          <el-switch v-model="form.enabled" />
        </el-form-item>
        <el-form-item label="Web Password">
          <el-input v-model="form.password" type="password" show-password />
        </el-form-item>
        <el-form-item label="frpc Token">
          <el-input v-model="form.token" type="password" show-password />
        </el-form-item>
        <el-form-item label="Port Pools">
          <el-input
            v-model="portsText"
            placeholder="10000-10999,12000"
          />
        </el-form-item>
        <el-form-item label="Max Ports">
          <el-input-number v-model="form.maxPorts" :min="0" style="width: 100%" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogVisible = false">Cancel</el-button>
        <el-button type="primary" @click="submit">Save</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { deleteUser, listUsers, saveUser, type PortRange, type UserRecord } from '../api/users'

const users = ref<UserRecord[]>([])
const loading = ref(false)
const dialogVisible = ref(false)
const editing = ref(false)
const portsText = ref('')

const form = reactive<UserRecord>({
  name: '',
  role: 'user',
  enabled: true,
  password: '',
  token: '',
  allowPorts: [],
  maxPorts: 0,
})

function formatPorts(ranges?: PortRange[]) {
  return (ranges || [])
    .map((item) => item.single || `${item.start}-${item.end}`)
    .join(', ')
}

function parsePorts(input: string): PortRange[] {
  return input
    .split(',')
    .map((item) => item.trim())
    .filter(Boolean)
    .map((item) => {
      if (item.includes('-')) {
        const [start, end] = item.split('-').map((v) => Number(v.trim()))
        return { start, end }
      }
      return { single: Number(item) }
    })
}

async function load() {
  loading.value = true
  try {
    users.value = await listUsers()
  } finally {
    loading.value = false
  }
}

function resetForm() {
  form.name = ''
  form.role = 'user'
  form.enabled = true
  form.password = ''
  form.token = ''
  form.allowPorts = []
  form.maxPorts = 0
  portsText.value = ''
}

function openCreate() {
  resetForm()
  editing.value = false
  dialogVisible.value = true
}

function openEdit(user: UserRecord) {
  form.name = user.name
  form.role = user.role || 'user'
  form.enabled = user.enabled
  form.password = ''
  form.token = ''
  form.allowPorts = user.allowPorts || []
  form.maxPorts = user.maxPorts || 0
  portsText.value = formatPorts(user.allowPorts)
  editing.value = true
  dialogVisible.value = true
}

async function submit() {
  await saveUser({
    ...form,
    allowPorts: parsePorts(portsText.value),
  })
  ElMessage.success('Saved')
  dialogVisible.value = false
  await load()
}

async function remove(name: string) {
  await ElMessageBox.confirm(`Delete user ${name}?`, 'Confirm', { type: 'warning' })
  await deleteUser(name)
  ElMessage.success('Deleted')
  await load()
}

onMounted(load)
</script>

<style scoped>
.users-page {
  display: flex;
  flex-direction: column;
  gap: 20px;
}

.page-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
}

.token-tag {
  margin-left: 6px;
}
</style>
