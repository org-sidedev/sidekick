<template>
  <KanbanBoard 
    v-if="store.workspaceId" 
    :tasks="tasks" 
    :showGuidedOverlay="showGuidedOverlay"
    @refresh="fetchTasks"
    @dismissOverlay="handleOverlayDismiss" 
  />
</template>

<script setup lang="ts">
import { ref, onMounted, watch, onUnmounted } from 'vue'
import type { Ref } from 'vue'
import { useRoute } from 'vue-router'
import KanbanBoard from '../components/KanbanBoard.vue'
import { store } from '../lib/store'
import type { FullTask } from '../lib/models'

const route = useRoute()

const parseTaskDates = (task: any): FullTask => {
  if (task.created) task.created = new Date(task.created)
  if (task.updated) task.updated = new Date(task.updated)
  if (task.archived) task.archived = new Date(task.archived)
  return task as FullTask
}

const tasks: Ref<Array<FullTask>> = ref([])
const showGuidedOverlay = ref(false) // disable for now, until this works better
//const showGuidedOverlay = ref(localStorage.getItem('guidedTourNeeded') !== 'false')
let socket: WebSocket | null = null
let socketClosed = false
let lastTaskStreamId: string | null = null

const fetchTasks = async () => {
  if (store.workspaceId) {
    try {
      const response = await fetch(`/api/v1/workspaces/${store.workspaceId}/tasks`)
      const data = await response.json()
      tasks.value = data.tasks.map((task: any) => parseTaskDates(task))
      if (tasks.value.length > 0) {
        // If there are any tasks, user never needs the guided tour again
        localStorage.setItem('guidedTourNeeded', 'false')
        showGuidedOverlay.value = false
      }
    } catch (error) {
      console.error('Failed to fetch tasks:', error)
    }
  }
}

const handleOverlayDismiss = () => {
  localStorage.setItem('guidedTourNeeded', 'false')
  showGuidedOverlay.value = false
}

const connectWebSocket = (onConnect: (() => void)) => {
  console.log("connectWebSocket task changes");
  const streamIdParam = lastTaskStreamId ? `?lastTaskStreamId=${lastTaskStreamId}` : '';
  console.log({streamIdParam});
  socket = new WebSocket(
    `ws://${window.location.host}/ws/v1/workspaces/${store.workspaceId}/task_changes${streamIdParam}`
  );

  socket.onopen = () => {
    console.log("WebSocket connection opened");
    onConnect();
  };

  socket.onmessage = (event) => {
    const data = JSON.parse(event.data);
    lastTaskStreamId = data.lastTaskStreamId;  // Update the last task stream ID
    updateTasks(data.tasks);
  };

  socket.onerror = (error) => {
    console.error("WebSocket error observed:", error);
    // You might want to attempt reconnection here
  };

  socket.onclose = (event) => {
    console.log("WebSocket closed:", event);
    if (socketClosed) {
      return;
    }
    console.log(
      "WebSocket is closed. Reconnect will be attempted in 1 second.",
      event.reason
    );
    setTimeout(() => {
      connectWebSocket(fetchTasks);
    }, 1000);
  };
};

const updateTasks = (newTasks: Array<FullTask>) => {
  // Merging or updating the current tasks based on new incoming tasks
  newTasks.forEach(task => {
    const parsedTask = parseTaskDates(task)
    const index = tasks.value.findIndex(t => t.id === parsedTask.id);
    if (index !== -1) {
      tasks.value.splice(index, 1, parsedTask);
    } else {
      tasks.value.push(parsedTask);
    }
  });

  console.debug('Updated tasks:', tasks.value);
}

const validateWorkspace = async (workspaceId: string): Promise<boolean> => {
  try {
    const response = await fetch(`/api/v1/workspaces/${workspaceId}`)
    return response.ok
  } catch (error) {
    console.error('Error validating workspace:', error)
    return false
  }
}

const initialize = async () => {
  connectWebSocket(fetchTasks);
}

const uninitialize = () => {
  console.log("uninitialize");
  if (socket !== null) {
    socketClosed = true
    socket.close()
    console.log("socket closed!");
  } else {
    console.log("socket is null");
  }
}

onMounted(async () => {
  const queryWorkspaceId = route.query.workspaceId as string
  
  if (queryWorkspaceId) {
    const previousWorkspaceId = store.workspaceId
    
    // Update store with query parameter workspace ID
    store.selectWorkspaceId(queryWorkspaceId)
    
    // Persist to storage
    sessionStorage.setItem('selectedWorkspaceId', queryWorkspaceId)
    localStorage.setItem('selectedWorkspaceId', queryWorkspaceId)
    
    // Validate workspace asynchronously
    const isValid = await validateWorkspace(queryWorkspaceId)
    
    if (!isValid) {
      // Revert to previous workspace selection
      if (previousWorkspaceId) {
        store.selectWorkspaceId(previousWorkspaceId)
        sessionStorage.setItem('selectedWorkspaceId', previousWorkspaceId)
        localStorage.setItem('selectedWorkspaceId', previousWorkspaceId)
      } else {
        // Clear invalid workspace selection
        store.selectWorkspaceId('')
        sessionStorage.removeItem('selectedWorkspaceId')
        localStorage.removeItem('selectedWorkspaceId')
      }
      
      window.alert(`Invalid workspace ID: ${queryWorkspaceId}`)
    }
  }
  
  await initialize()
});
onUnmounted(uninitialize);

watch(() => store.workspaceId, () => {
  uninitialize()
  initialize()
})

</script>