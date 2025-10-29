import React, { useState, useEffect } from 'react';
import { BACKEND_URL } from '../config';

interface JournalEntry {
  operation: string;
  path: string;
  content: string;
  timestamp: string;
  user: string;
  permissions: string;
}

interface JournalingViewerProps {
  partitionId: string;
  onClose: () => void;
}

const JournalingViewer: React.FC<JournalingViewerProps> = ({ partitionId, onClose }) => {
  const [entries, setEntries] = useState<JournalEntry[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState('');
  const [filter, setFilter] = useState('');

  // BACKEND_URL provided by src/config.ts

  useEffect(() => {
    console.log('JournalingViewer montado con partitionId:', partitionId);
    // Defensive: if no partitionId was provided, don't call the backend
    if (!partitionId || !partitionId.trim()) {
      setError('Selecciona una partición montada o inicia sesión en una partición');
      setIsLoading(false);
      return;
    }

    loadJournaling();
  }, [partitionId]);

  const loadJournaling = async () => {
    console.log('Cargando journaling para partición:', partitionId);
    setIsLoading(true);
    setError('');
    if (!partitionId || !partitionId.trim()) {
      setError('Selecciona una partición montada o inicia sesión en una partición');
      setIsLoading(false);
      return;
    }
    try {
      const response = await fetch(`${BACKEND_URL}/journaling`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ partitionId }),
      });

      console.log('Response status:', response.status);

      if (response.ok) {
        const data = await response.json();
        console.log('Data recibida:', data);
        
        if (data.success) {
          const formattedEntries = (data.entries || []).map((entry: JournalEntry) => {
            // Convertir Unix timestamp a fecha legible
            const timestamp = parseInt(entry.timestamp);
            const date = new Date(timestamp * 1000); // Multiplicar por 1000 para convertir a milisegundos
            
            return {
              ...entry,
              timestamp: date.toLocaleString('es-ES', {
                year: 'numeric',
                month: '2-digit',
                day: '2-digit',
                hour: '2-digit',
                minute: '2-digit',
                second: '2-digit'
              }),
              content: entry.content || '(sin contenido)', // Mostrar mensaje si está vacío
            };
          });
          
          setEntries(formattedEntries);
          console.log('Entradas cargadas:', formattedEntries.length);
        } else {
          setError(data.error || 'Error al cargar el journaling');
          console.error('Error del servidor:', data.error);
        }
      } else {
        setError('Error de conexión con el servidor');
        console.error('Error HTTP:', response.status);
      }
    } catch (err) {
      setError('Error al cargar el journaling');
      console.error('Error catch:', err);
    } finally {
      setIsLoading(false);
    }
  };

  const filteredEntries = entries.filter(entry =>
    entry.operation.toLowerCase().includes(filter.toLowerCase()) ||
    entry.path.toLowerCase().includes(filter.toLowerCase())
  );

  const getOperationIcon = (operation: string) => {
    const op = operation.toLowerCase();
    if (op.includes('crear') || op.includes('create') || op.includes('mkfile') || op.includes('mkdir')) return '📝';
    if (op.includes('eliminar') || op.includes('delete') || op.includes('remove')) return '🗑️';
    if (op.includes('modificar') || op.includes('edit')) return '✏️';
    if (op.includes('renombrar') || op.includes('rename')) return '📛';
    if (op.includes('mover') || op.includes('move')) return '📦';
    if (op.includes('copiar') || op.includes('copy')) return '📋';
    return '📄';
  };

  const getOperationColor = (operation: string) => {
    const op = operation.toLowerCase();
    if (op.includes('crear') || op.includes('create') || op.includes('mkfile') || op.includes('mkdir')) return '#4caf50';
    if (op.includes('eliminar') || op.includes('delete') || op.includes('remove')) return '#f44336';
    if (op.includes('modificar') || op.includes('edit')) return '#ff9800';
    return '#2196f3';
  };

  console.log('Renderizando JournalingViewer');

  return (
    <div className="modal-overlay" onClick={onClose}>
      <div className="modal-content journaling-modal" onClick={(e) => e.stopPropagation()}>
        <div className="modal-header">
          <h2>
            <span className="modal-icon">📜</span>
            Registro de Journaling
          </h2>
          <button className="modal-close" onClick={onClose}>✕</button>
        </div>

        <div className="journaling-controls">
          <div className="search-box">
            <span className="search-icon">🔍</span>
            <input
              type="text"
              placeholder="Filtrar por operación o ruta..."
              value={filter}
              onChange={(e) => setFilter(e.target.value)}
              className="search-input"
            />
          </div>
          <button className="btn btn-secondary btn-sm" onClick={loadJournaling}>
            🔄 Recargar
          </button>
        </div>

        <div className="modal-body journaling-body">
          {isLoading ? (
            <div className="journaling-loading">
              <div className="loader"></div>
              <p>Cargando registro de journaling...</p>
            </div>
          ) : error ? (
            <div className="journaling-error">
              <span className="error-icon">⚠️</span>
              <p>{error}</p>
              <button className="btn btn-primary" onClick={loadJournaling}>
                Reintentar
              </button>
            </div>
          ) : filteredEntries.length === 0 ? (
            <div className="empty-state">
              <div className="empty-icon">📜</div>
              <p>No hay entradas en el journaling</p>
              <small>Las operaciones se registrarán aquí automáticamente</small>
            </div>
          ) : (
            <div className="journaling-timeline">
              {filteredEntries.map((entry, index) => (
                <div key={index} className="journal-entry">
                  <div className="entry-marker" style={{ background: getOperationColor(entry.operation) }}>
                    <span>{getOperationIcon(entry.operation)}</span>
                  </div>
                  <div className="entry-content">
                    <div className="entry-header">
                      <span 
                        className="entry-operation"
                        style={{ color: getOperationColor(entry.operation) }}
                      >
                        {entry.operation}
                      </span>
                      <span className="entry-timestamp">{entry.timestamp}</span>
                    </div>
                    <div className="entry-path">
                      📂 {entry.path || 'N/A'}
                    </div>
                {entry.content && entry.content !== '(sin contenido)' ? (
                  <div className="entry-details">
                    <strong>Contenido:</strong>
                    <pre className="entry-content-preview">
                      {entry.content.substring(0, 100)}
                      {entry.content.length > 100 ? '...' : ''}
                    </pre>
                  </div>
                ) : (
                  <div className="entry-details" style={{ fontStyle: 'italic', color: '#999' }}>
                    {entry.operation.toLowerCase().includes('mkfile') || 
                     entry.operation.toLowerCase().includes('mkdir') ? (
                      <span>📝 Archivo/directorio creado sin contenido inicial</span>
                    ) : entry.operation.toLowerCase().includes('remove') ? (
                      <span>🗑️ Elemento eliminado</span>
                    ) : (
                      <span>ℹ️ Sin contenido adicional</span>
                    )}
                  </div>
                )}
              </div>
            </div>
          ))}
        </div>
          )}
        </div>

        <div className="modal-footer">
          <div className="journaling-stats">
            <span>📊 Total de entradas: {filteredEntries.length}</span>
            {filter && <span>🔍 Filtradas: {filteredEntries.length} de {entries.length}</span>}
          </div>
          <button className="btn btn-secondary" onClick={onClose}>
            Cerrar
          </button>
        </div>
      </div>
    </div>
  );
};

export default JournalingViewer;