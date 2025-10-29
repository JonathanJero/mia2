import React, { useState, useEffect } from 'react';
import '../App.css';
import { BACKEND_URL } from '../config';

interface LoginProps {
  onLogin: (partitionId: string, username: string, isRoot: boolean) => void;
  onClose: () => void;
}

interface Disk {
  path: string;
  size: number;
  unit: string;
  fit: string;
  partitions: Partition[];
}

interface Partition {
  name: string;
  id: string;
  size: number;
  type: string;
  isMounted: boolean;
  status: string;
}

const Login: React.FC<LoginProps> = ({ onLogin, onClose }) => {
  const [disks, setDisks] = useState<Disk[]>([]);
  const [selectedPartition, setSelectedPartition] = useState('');
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [isLoading, setIsLoading] = useState(false);
  const [isLoadingDisks, setIsLoadingDisks] = useState(true);
  const [error, setError] = useState('');

  // BACKEND_URL provided by src/config.ts

  useEffect(() => {
    loadDisks();
  }, []);

  const loadDisks = async () => {
    setIsLoadingDisks(true);
    setError('');
    
    try {
      const response = await fetch(`${BACKEND_URL}/disks/mounted`);
      
      if (response.ok) {
        const data = await response.json();
        setDisks(data.disks || []);
        
        // Auto-seleccionar primera partición montada
        if (data.disks && data.disks.length > 0) {
          for (const disk of data.disks) {
            const mountedPartition = disk.partitions.find((p: Partition) => p.isMounted);
            if (mountedPartition) {
              setSelectedPartition(mountedPartition.id);
              break;
            }
          }
        }
      } else {
        setError('Error al cargar los discos');
      }
    } catch (err) {
      setError('Error de conexión con el servidor');
    } finally {
      setIsLoadingDisks(false);
    }
  };

  const getMountedPartitions = (): Partition[] => {
    const mounted: Partition[] = [];
    disks.forEach(disk => {
      disk.partitions.forEach(partition => {
        if (partition.isMounted) {
          mounted.push(partition);
        }
      });
    });
    return mounted;
  };

  const handleLogin = async (e: React.FormEvent) => {
    e.preventDefault();

    if (!username || !password) {
      setError('Todos los campos son obligatorios');
      return;
    }

    setIsLoading(true);
    setError('');

    try {
      const command = `login -user=${username} -pass=${password} -id=${selectedPartition}`;

      const response = await fetch(`${BACKEND_URL}/execute`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ command }),
      });

      if (response.ok) {
        const result = await response.json();

        if (result.success) {
          const isRoot = username.toLowerCase() === 'root';
          onLogin(selectedPartition, username, isRoot);  // Pasar isRoot
          onClose();
        } else {
          setError(result.error || 'Credenciales incorrectas');
        }
      } else {
        setError('Error de conexión con el servidor');
      }
    } catch (err) {
      setError('No se pudo conectar con el backend');
    } finally {
      setIsLoading(false);
    }
  };

  const mountedPartitions = getMountedPartitions();

  return (
    <div className="modal-overlay" onClick={onClose}>
      <div className="modal-content login-modal" onClick={(e) => e.stopPropagation()}>
        <div className="modal-header">
          <h2>🔐 Iniciar Sesión en el Sistema de Archivos</h2>
          <button className="modal-close" onClick={onClose}>×</button>
        </div>

        <div className="modal-body">
          {isLoadingDisks ? (
            <div className="loading-state">
              <div className="spinner"></div>
              <p>Cargando particiones disponibles...</p>
            </div>
          ) : (
            <form onSubmit={handleLogin} className="login-form">
              {error && (
                <div className="alert alert-error">
                  <span className="alert-icon">⚠️</span>
                  <span>{error}</span>
                </div>
              )}

              <div className="form-section">
                <h3>Selecciona una Partición</h3>
                <div className="partitions-select">
                  {mountedPartitions.map((partition) => (
                    <label
                      key={partition.id}
                      className={`partition-option ${selectedPartition === partition.id ? 'selected' : ''}`}
                    >
                      <input
                        type="radio"
                        name="partition"
                        value={partition.id}
                        checked={selectedPartition === partition.id}
                        onChange={(e) => setSelectedPartition(e.target.value)}
                      />
                      <div className="partition-info">
                        <span className="partition-icon">💾</span>
                        <div>
                          <div className="partition-name">{partition.name}</div>
                          <div className="partition-id">ID: {partition.id}</div>
                        </div>
                      </div>
                      <span className="check-icon">✓</span>
                    </label>
                  ))}
                </div>
              </div>

              <div className="form-section">
                <h3>Credenciales</h3>
                <div className="form-group">
                  <label htmlFor="username">Usuario</label>
                  <input
                    id="username"
                    type="text"
                    value={username}
                    onChange={(e) => setUsername(e.target.value)}
                    placeholder="Ingresa tu usuario"
                    className="form-input"
                    autoComplete="username"
                  />
                </div>

                <div className="form-group">
                  <label htmlFor="password">Contraseña</label>
                  <input
                    id="password"
                    type="password"
                    value={password}
                    onChange={(e) => setPassword(e.target.value)}
                    placeholder="Ingresa tu contraseña"
                    className="form-input"
                    autoComplete="current-password"
                  />
                </div>
              </div>

              <div className="form-hint">
                💡 <strong>Tip:</strong> El usuario root tiene acceso total al sistema de archivos.
              </div>

              <div className="modal-footer">
                <button
                  type="button"
                  className="btn btn-secondary"
                  onClick={onClose}
                  disabled={isLoading}
                >
                  Cancelar
                </button>
                <button
                  type="submit"
                  className="btn btn-primary"
                  disabled={isLoading || !username || !password}
                >
                  {isLoading ? (
                    <>
                      <span className="btn-spinner"></span>
                      Iniciando sesión...
                    </>
                  ) : (
                    <>
                      <span>🔓</span>
                      Iniciar Sesión
                    </>
                  )}
                </button>
              </div>
            </form>
          )}
        </div>
      </div>
    </div>
  );
};

export default Login;