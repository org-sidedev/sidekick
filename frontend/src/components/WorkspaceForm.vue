<template>
  <form @submit.prevent="submitWorkspace">
    <div>
      <label for="name">Workspace Name:</label>
      <input id="name" v-model="name" required>
    </div>
    <div>
      <label for="localRepoDir">Local Repository Directory:</label>
      <input id="localRepoDir" v-model="localRepoDir" required>
    </div>
    <div>
      <h3>LLMs</h3>
      <div v-for="(config, index) in llmConfig.defaults" :key="index" class="config-item">
        <label :for="'llmProvider' + index">Provider:</label>
        <select :id="'llmProvider' + index" v-model="config.provider" required>
          <option value="">Select</option>
          <option value="openai">OpenAI</option>
          <option value="anthropic">Anthropic</option>
        </select>
        <!--label :for="'llmModel' + index">LLM Model:</label>
        <input :id="'llmModel' + index" v-model="config.model" required-->
        <button type="button" @click="removeConfig('llm', index)" v-if="llmConfig.defaults.length > 1" class="remove-btn">Remove</button>
      </div>
      <button type="button" @click="addConfig('llm')" class="add-btn">Add Fallback</button>
    </div>
    <div>
      <h3>Embeddings</h3>
      <div v-for="(config, index) in embeddingConfig.defaults" :key="index" class="config-item">
        <label :for="'embeddingProvider' + index">Provider:</label>
        <select :id="'embeddingProvider' + index" v-model="config.provider" required>
          <option value="">Select</option>
          <option value="openai">OpenAI</option>
        </select>
        <!--label :for="'embeddingModel' + index">Embedding Model:</label>
        <input :id="'embeddingModel' + index" v-model="config.model" required-->
        <button type="button" @click="removeConfig('embedding', index)" v-if="embeddingConfig.defaults.length > 1" class="remove-btn">Remove</button>
      </div>
      <button type="button" @click="addConfig('embedding')" class="add-btn">Add Fallback</button>
    </div>
    <button type="submit">{{ isEditing ? 'Update' : 'Create' }} Workspace</button>
  </form>
</template>

<script setup lang="ts">
import { ref, computed, watch, onMounted } from 'vue';
import type { Workspace, LLMConfig, EmbeddingConfig } from '@/lib/models';

const props = defineProps<{
  workspace: Workspace;
}>();

const emit = defineEmits<{
  (event: 'created', id: string): void;
  (event: 'updated', id: string): void;
}>();

const name = ref('');
const localRepoDir = ref('');
const llmConfig = ref<LLMConfig>({ defaults: [{ provider: '', model: '' }], useCaseConfigs: {} });
const embeddingConfig = ref<EmbeddingConfig>({ defaults: [{ provider: '', model: '' }], useCaseConfigs: {} });

const isEditing = computed(() => !!props.workspace.id);

onMounted(() => {
  if (props.workspace) {
    name.value = props.workspace.name;
    localRepoDir.value = props.workspace.localRepoDir;
    if (props.workspace.llmConfig != null && props.workspace.llmConfig.defaults.length > 0) {
      llmConfig.value = props.workspace.llmConfig;
    }
    if (props.workspace.embeddingConfig != null && props.workspace.embeddingConfig.defaults.length > 0) {
      embeddingConfig.value = props.workspace.embeddingConfig;
    }
  }
});

const addConfig = (type: 'llm' | 'embedding') => {
  const config = type === 'llm' ? llmConfig : embeddingConfig;
  config.value.defaults.push({ provider: '', model: '' });
};

const removeConfig = (type: 'llm' | 'embedding', index: number) => {
  const config = type === 'llm' ? llmConfig : embeddingConfig;
  if (config.value.defaults.length > 1) {
    config.value.defaults.splice(index, 1);
  }
};

const submitWorkspace = async () => {
  const formData: Omit<Workspace, 'id'> = {
    name: name.value,
    localRepoDir: localRepoDir.value,
    llmConfig: llmConfig.value,
    embeddingConfig: embeddingConfig.value
  };

  try {
    const url = isEditing.value ? `/api/v1/workspaces/${props.workspace?.id}` : '/api/v1/workspaces';
    const method = isEditing.value ? 'PUT' : 'POST';

    const response = await fetch(url, {
      method,
      headers: {
        'Content-Type': 'application/json'
      },
      body: JSON.stringify(formData)
    });

    if (response.ok) {
      const newWorkspace: Workspace = (await response.json()).workspace;
      if (isEditing.value) {
        emit('updated', newWorkspace.id as string);
      } else {
        emit('created', newWorkspace.id as string);
      }
    } else {
      console.error(`Failed to ${isEditing.value ? 'update' : 'create'} workspace:`, response.status);
    }
  } catch (error) {
    console.error(`Failed to ${isEditing.value ? 'update' : 'create'} workspace:`, error);
  }
};
</script>

<style scoped>
form {
  display: flex;
  flex-direction: column;
  gap: 1rem;
}

.config-item {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  margin-bottom: 0.5rem;
}

.add-btn, .remove-btn {
  padding: 0.25rem 0.5rem;
  font-size: 0.875rem;
  cursor: pointer;
}

.add-btn {
  margin-top: 0.5rem;
}

.remove-btn {
  background-color: var(--color-danger);
  color: var(--color-text-inverse);
  border: none;
  border-radius: 0.25rem;
}
</style>