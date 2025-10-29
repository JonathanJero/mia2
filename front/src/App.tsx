import React, { useState, useRef, useEffect } from 'react';
import Login from './components/Login';
import FileSystemViewer from './components/FileSystemViewer';
import JournalingViewer from './components/JournalingViewer';
import './App.css';
import { BACKEND_URL } from './config';

interface CommandResult {
  command: string;
  output: string;
  timestamp: string;
  isError?: boolean;
}

interface BackendResponse {
  success: boolean;
  output: string;
  error?: string;
}

interface Session {
  partitionId: string;
  username: string;
  isLoggedIn: boolean;
  isRoot: boolean;
}

const App: React.FC = () => {
  const [commands, setCommands] = useState<string>('');
  const [output, setOutput] = useState<CommandResult[]>([]);
  const [isExecuting, setIsExecuting] = useState(false);
  const [isLoading, setIsLoading] = useState(false);
  const [isConnected, setIsConnected] = useState(false);
  const [showLogin, setShowLogin] = useState(false);
  const [showViewer, setShowViewer] = useState(false);
  const [showJournaling, setShowJournaling] = useState(false);
  const [session, setSession] = useState<Session>({
    partitionId: '',
    username: '',
    isLoggedIn: false,
    isRoot: false
  });

  const fileInputRef = useRef<HTMLInputElement>(null);
  const outputRef = useRef<HTMLDivElement>(null);
  const textareaRef = useRef<HTMLTextAreaElement>(null);

  // BACKEND_URL is provided by src/config.ts (VITE_BACKEND_URL or fallback to localhost:8080)

  // Comandos que requieren autenticación
  const COMMANDS_REQUIRING_AUTH = [
    'mkgrp', 'rmgrp', 'mkusr', 'rmusr',
    'mkdir', 'mkfile', 'remove', 'edit',
    'rename', 'copy', 'move', 'find',
    'chown', 'chmod', 'cat', 'recovery', 'loss'
  ];

  useEffect(() => {
    checkBackendConnection();
  }, []);

  useEffect(() => {
    if (outputRef.current) {
      outputRef.current.scrollTop = outputRef.current.scrollHeight;
    }
  }, [output]);

  const checkBackendConnection = async () => {
    try {
      const response = await fetch(`${BACKEND_URL}/health`);
      if (response.ok) {
        setIsConnected(true);
        addToOutput('', '🟢 Conectado al backend', false);
        // Intentar sincronizar la sesión del servidor con el frontend
        fetchSessionInfo();
      } else {
        setIsConnected(false);
        addToOutput('', '🔴 Error de conexión con el backend', true);
      }
    } catch (error) {
      setIsConnected(false);
      addToOutput('', '🔴 Backend no disponible. Asegúrate de que esté ejecutándose.', true);
    }
  };

  const fetchSessionInfo = async () => {
    try {
      const resp = await fetch(`${BACKEND_URL}/session`);
      if (!resp.ok) return;
      const data = await resp.json();
      if (data && data.session && data.session.isLoggedIn) {
        const s = data.session;
        setSession({
          partitionId: s.partitionId || '',
          username: s.username || '',
          isLoggedIn: !!s.isLoggedIn,
          isRoot: !!s.isRoot,
        });
        addToOutput('', `🔁 Sesión sincronizada: ${s.username}@${s.partitionId}`, false);
      }
    } catch (e) {
      // ignore
    }
  };

  const handleFileLoad = (event: React.ChangeEvent<HTMLInputElement>) => {
    const file = event.target.files?.[0];
    if (!file) return;

    if (!file.name.endsWith('.smia')) {
      addToOutput('', 'Error: Solo se permiten archivos con extensión .smia', true);
      return;
    }

    setIsLoading(true);
    const reader = new FileReader();
    
    reader.onload = (e) => {
      const content = e.target?.result as string;
      setCommands(content);
      addToOutput('', `✅ Archivo "${file.name}" cargado exitosamente`, false);
      setIsLoading(false);
    };

    reader.onerror = () => {
      addToOutput('', 'Error al leer el archivo', true);
      setIsLoading(false);
    };

    reader.readAsText(file);
  };

  const addToOutput = (command: string, result: string, isError: boolean = false) => {
    const timestamp = new Date().toLocaleTimeString();
    setOutput(prev => [...prev, {
      command,
      output: result,
      timestamp,
      isError
    }]);
  };

  const requiresAuth = (command: string): boolean => {
    const cmdName = command.trim().toLowerCase().split(/\s+/)[0];
    return COMMANDS_REQUIRING_AUTH.includes(cmdName);
  };

  const executeCommands = async () => {
    if (!commands.trim()) {
      addToOutput('', 'No hay comandos para ejecutar', true);
      return;
    }

    if (!isConnected) {
      addToOutput('', 'No hay conexión con el backend. Verifica que esté ejecutándose.', true);
      return;
    }

    const commandLines = commands.split('\n').filter(line => line.trim());
    
    // Verificar si algún comando requiere autenticación
    const needsAuth = commandLines.some(cmd => 
      !cmd.trim().startsWith('#') && 
      cmd.trim() && 
      requiresAuth(cmd)
    );

    if (needsAuth && !session.isLoggedIn) {
      addToOutput('', '⚠️ Algunos comandos requieren autenticación. Por favor, inicia sesión.', true);
      setShowLogin(true);
      return;
    }

    setIsExecuting(true);

    for (const command of commandLines) {
      if (command.trim().startsWith('#') || !command.trim()) continue;

      // Saltar comando login si ya está en la sesión
      if (command.trim().toLowerCase().startsWith('login')) {
        addToOutput(command.trim(), 'ℹ️ Ya hay una sesión activa', false);
        continue;
      }

      try {
        addToOutput(command.trim(), 'Ejecutando...', false);
        
        const response = await fetch(`${BACKEND_URL}/execute`, {
          method: 'POST',
          headers: {
            'Content-Type': 'application/json',
          },
          body: JSON.stringify({ command: command.trim() }),
        });

        if (response.ok) {
          const result: BackendResponse = await response.json();
          
          setOutput(prev => prev.slice(0, -1));
          
          if (result.success) {
            addToOutput(command.trim(), result.output || '✅ Comando ejecutado exitosamente', false);
          } else {
            addToOutput(command.trim(), result.error || 'Error desconocido', true);
          }
        } else {
          setOutput(prev => prev.slice(0, -1));
          addToOutput(command.trim(), `Error HTTP: ${response.status} - ${response.statusText}`, true);
        }
      } catch (error) {
        setOutput(prev => prev.slice(0, -1));
        addToOutput(command.trim(), `Error de conexión: ${error}`, true);
      }

      await new Promise(resolve => setTimeout(resolve, 300));
    }

    setIsExecuting(false);
  };

  const handleLogin = (partitionId: string, username: string, isRoot: boolean) => {
    setSession({
      partitionId,
      username,
      isLoggedIn: true,
      isRoot,
    });
    addToOutput('', `✅ Sesión iniciada: ${username}@${partitionId}`, false);
  };

  const handleLogout = async () => {
    if (!session.isLoggedIn) return;

    try {
      const response = await fetch(`${BACKEND_URL}/execute`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
        },
        body: JSON.stringify({ command: 'logout' }),
      });

      if (response.ok) {
        setSession({
          partitionId: '',
          username: '',
          isLoggedIn: false,
          isRoot: false,
        });
        addToOutput('logout', '✅ Sesión cerrada exitosamente', false);
      }
    } catch (error) {
      addToOutput('logout', 'Error al cerrar sesión', true);
    }
  };

  const clearOutput = () => {
    setOutput([]);
  };

  const triggerFileInput = () => {
    fileInputRef.current?.click();
  };

  const reconnectBackend = () => {
    addToOutput('', 'Intentando reconectar...', false);
    checkBackendConnection();
  };

  return (
    <div className="app">
      {/* Login Modal */}
      {showLogin && (
        <Login 
          onLogin={handleLogin}
          onClose={() => setShowLogin(false)}
        />
      )}

      {/* File System Viewer Modal */}
      {showViewer && (
        <FileSystemViewer 
          session={session}
          onClose={() => setShowViewer(false)}
        />
      )}

      {/* Journaling Viewer Modal */}
      {showJournaling && session.isLoggedIn && session.partitionId && session.partitionId.trim() && (
        <JournalingViewer
          partitionId={session.partitionId}
          onClose={() => setShowJournaling(false)}
        />
      )}

      {/* Header */}
      <header className="header">
        <div className="header-content">
          <h1 className="title">
            <span className="title-icon">📚</span>
            Proyecto 2 MIA
          </h1>
          <div className="header-actions">
            <div className="status-indicator">
              <div className={`status-dot ${isExecuting ? 'executing' : isConnected ? 'ready' : 'error'}`}></div>
              <span className="status-text">
                {isExecuting ? 'Ejecutando...' : isConnected ? 'Conectado' : 'Desconectado'}
              </span>
              {!isConnected && (
                <button className="btn btn-sm btn-secondary" onClick={reconnectBackend}>
                  Reconectar
                </button>
              )}
            </div>
              {session.isLoggedIn ? (
              <>
                <button 
                  className="btn btn-sm btn-viewer" 
                  onClick={() => setShowViewer(true)}
                >
                  💾 Visualizador
                </button>
                <button 
                  className="btn btn-sm btn-journaling" 
                  onClick={() => setShowJournaling(true)}
                  disabled={!session.partitionId || !session.partitionId.trim()}
                  title={(!session.partitionId || !session.partitionId.trim()) ? 'Selecciona una partición montada para ver el journaling' : 'Abrir Journaling'}
                >
                  📜 Journaling
                </button>
                <div className="session-info">
                  <span className="session-user">
                    {session.username}@{session.partitionId}
                  </span>
                  <button className="btn btn-sm btn-logout" onClick={handleLogout}>
                    Cerrar Sesión
                  </button>
                </div>
              </>
            ) : (
              <button className="btn btn-sm btn-primary" onClick={() => setShowLogin(true)}>
                Iniciar Sesión
              </button>
            )}            
          </div>
        </div>
      </header>

      {/* Main Content */}
      <main className="main-content">
        {/* Left Panel - Commands */}
        <div className="panel commands-panel">
          <div className="panel-header">
            <h2 className="panel-title">
              <span className="panel-icon">📝</span>
              Comandos
            </h2>
            <div className="panel-actions">
              <button 
                className="btn btn-secondary"
                onClick={triggerFileInput}
                disabled={isLoading}
              >
                {isLoading ? (
                  <>
                    <span className="btn-icon loading">⏳</span>
                    Cargando...
                  </>
                ) : (
                  <>
                    <span className="btn-icon">📁</span>
                    Cargar .smia
                  </>
                )}
              </button>
              <input
                ref={fileInputRef}
                type="file"
                accept=".smia"
                onChange={handleFileLoad}
                style={{ display: 'none' }}
              />
            </div>
          </div>
          
          <div className="textarea-container">
            <textarea
              ref={textareaRef} 
              className="commands-textarea"
              value={commands}
              onChange={(e) => setCommands(e.target.value)}
              placeholder="# Escribe tus comandos aquí..."
              spellCheck={false}
            />
          </div>

          <div className="panel-footer">
            <button
              className="btn btn-primary"
              onClick={executeCommands}
              disabled={isExecuting || !commands.trim() || !isConnected}
            >
              {isExecuting ? (
                <>
                  <span className="btn-icon executing">⚡</span>
                  Ejecutando...
                </>
              ) : (
                <>
                  <span className="btn-icon">▶️</span>
                  Ejecutar
                </>
              )}
            </button>
          </div>
        </div>

        {/* Right Panel - Output */}
        <div className="panel output-panel">
          <div className="panel-header">
            <h2 className="panel-title">
              <span className="panel-icon">🖥️</span>
              Salida del Terminal
            </h2>
            <div className="panel-actions">
              <button 
                className="btn btn-danger"
                onClick={clearOutput}
                disabled={output.length === 0}
              >
                <span className="btn-icon">🗑️</span>
                Limpiar
              </button>
            </div>
          </div>

          <div className="output-container" ref={outputRef}>
            {output.length === 0 ? (
              <div className="output-empty">
                <div className="empty-icon">🚀</div>
                <p>No hay salida que mostrar</p>
                <small>Ejecuta algunos comandos para ver los resultados aquí</small>
              </div>
            ) : (
              output.map((result, index) => (
                <div 
                  key={index} 
                  className={`output-entry ${result.isError ? 'error' : 'success'}`}
                >
                  {result.command && (
                    <div className="command-line">
                      <span className="prompt">➤</span>
                      <span className="command">{result.command}</span>
                      <span className="timestamp">{result.timestamp}</span>
                    </div>
                  )}
                  <div className="output-line">
                    <pre>{result.output}</pre>
                  </div>
                </div>
              ))
            )}
          </div>
        </div>
      </main>

      {/* Footer */}
      <footer className="footer">
        <div className="footer-content">
          <p>Proyecto 2 MIA @ 2025 - {isConnected ? '🟢 Conectado' : '🔴 Desconectado'}</p>
          <div className="footer-stats">
            <span>Comandos: {commands.split('\n').filter(line => line.trim() && !line.trim().startsWith('#')).length}</span>
            <span>Salidas: {output.length}</span>
            {session.isLoggedIn && <span>👤 {session.username}</span>}
          </div>
        </div>
      </footer>
    </div>
  );
};

export default App;