import React, { useState, useEffect } from 'react';
import '../App.css';
import { BACKEND_URL } from '../config';

interface Partition {
  name: string;
  id: string;
  size: number;
  type: string;
  isMounted: boolean;
  status: string;
}

interface FileExplorerProps {
  partition: Partition;
  session: {
    partitionId: string;
    username: string;
    isLoggedIn: boolean;
    isRoot: boolean;
  };
  onBack: () => void;
}

interface FileNode {
  name: string;
  type: 'file' | 'folder';
  size: number;
  permissions: string;
  owner: string;
  group: string;
  content?: string;
  children?: FileNode[];
}

const FileExplorer: React.FC<FileExplorerProps> = ({ partition }) => {
  const [currentPath, setCurrentPath] = useState('/');
  const [files, setFiles] = useState<FileNode[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState('');
  const [selectedFile, setSelectedFile] = useState<FileNode | null>(null);
  const [fileContent, setFileContent] = useState('');
  const [isLoadingContent, setIsLoadingContent] = useState(false);
  const [showModal, setShowModal] = useState(false);

  // BACKEND_URL imported from config

  useEffect(() => {
    loadFiles(currentPath);
  }, [currentPath, partition.id]);

  const loadFiles = async (path: string) => {
    setIsLoading(true);
    setError('');

    try {
      const response = await fetch(`${BACKEND_URL}/files`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          partitionId: partition.id,
          path: path,
        }),
      });

      if (response.ok) {
        const data = await response.json();
        setFiles(data.files || []);
      } else {
        setError('Error al cargar los archivos');
      }
    } catch (err) {
      setError('Error de conexión con el servidor');
    } finally {
      setIsLoading(false);
    }
  };

  const readFileContent = async (fileName: string) => {
    setIsLoadingContent(true);
    setFileContent('');

    const filePath = currentPath === '/' 
      ? `/${fileName}` 
      : `${currentPath}/${fileName}`;

    try {
      const response = await fetch(`${BACKEND_URL}/file/read`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          partitionId: partition.id,
          path: filePath,
        }),
      });

      if (response.ok) {
        const data = await response.json();
        if (data.success) {
          setFileContent(data.content);
        } else {
          setFileContent(`Error: ${data.error}`);
        }
      } else {
        setFileContent('Error al leer el archivo');
      }
    } catch (err) {
      setFileContent('Error de conexión con el servidor');
    } finally {
      setIsLoadingContent(false);
    }
  };

  const handleFileClick = async (file: FileNode) => {
    if (file.type === 'folder') {
      navigateToFolder(file.name);
    } else {
      setSelectedFile(file);
      setShowModal(true);
      await readFileContent(file.name);
    }
  };

  const navigateToFolder = (folderName: string) => {
    const newPath = currentPath === '/' 
      ? `/${folderName}` 
      : `${currentPath}/${folderName}`;
    setCurrentPath(newPath);
  };

  const navigateUp = () => {
    if (currentPath === '/') return;
    const parts = currentPath.split('/').filter(p => p);
    parts.pop();
    setCurrentPath(parts.length === 0 ? '/' : '/' + parts.join('/'));
  };

  const getFileIcon = (file: FileNode) => {
    if (file.type === 'folder') return '📁';
    
    const ext = file.name.split('.').pop()?.toLowerCase();
    switch (ext) {
      case 'txt': return '📄';
      case 'jpg':
      case 'jpeg':
      case 'png':
      case 'gif': return '🖼️';
      case 'pdf': return '📕';
      case 'zip':
      case 'rar': return '📦';
      default: return '📄';
    }
  };

  const closeModal = () => {
    setShowModal(false);
    setSelectedFile(null);
    setFileContent('');
  };

  return (
    <div className="file-explorer">
      <div className="explorer-header">
        <div className="explorer-info">
          <h3>📁 {partition.name}</h3>
          <p className="current-path">Ruta: {currentPath}</p>
        </div>
        
        <div className="explorer-actions">
          {currentPath !== '/' && (
            <button className="btn btn-secondary" onClick={navigateUp}>
              ⬆️ Subir
            </button>
          )}
          <button className="btn btn-secondary" onClick={() => loadFiles(currentPath)}>
            🔄 Recargar
          </button>
        </div>
      </div>

      <div className="explorer-body">
        {isLoading ? (
          <div className="explorer-loading">
            <div className="loader"></div>
            <p>Cargando archivos...</p>
          </div>
        ) : error ? (
          <div className="explorer-error">
            <span className="error-icon">⚠️</span>
            <p className='m-3'>{error}</p>
            <button className="btn btn-primary" onClick={() => loadFiles(currentPath)}>
              Reintentar
            </button>
          </div>
        ) : files.length === 0 ? (
          <div className="empty-state">
            <div className="empty-icon">📂</div>
            <p className='p-modals'>Esta carpeta está vacía</p>
          </div>
        ) : (
          <div className="files-list">
            {files.map((file, index) => (
              <div
                key={index}
                className={`file-item ${file.type}`}
                onClick={() => handleFileClick(file)}
                style={{ cursor: 'pointer' }}
              >
                <span className="file-icon">{getFileIcon(file)}</span>
                <div className="file-info">
                  <span className="file-name">{file.name}</span>
                  <div className="file-meta">
                    <span className="file-size">
                      {file.size > 1024 
                        ? `${(file.size / 1024).toFixed(2)} KB` 
                        : `${file.size} B`}
                    </span>
                    <span className="file-permissions">{file.permissions}</span>
                    <span className="file-owner">👤 {file.owner}</span>
                    <span className="file-group">👥 {file.group}</span>
                  </div>
                </div>
              </div>
            ))}
          </div>
        )}
      </div>

      {/* Modal para visualizar contenido del archivo */}
      {showModal && selectedFile && (
        <div className="modal-overlay" onClick={closeModal}>
          <div className="modal-content" onClick={(e) => e.stopPropagation()}>
            <div className="modal-header">
              <h3>
                <span className="file-icon">{getFileIcon(selectedFile)}</span>
                {selectedFile.name}
              </h3>
              <button className="modal-close" onClick={closeModal}>✕</button>
            </div>
            <div className="modal-body">
              {isLoadingContent ? (
                <div className="modal-loading">
                  <div className="loader"></div>
                  <p>Cargando contenido...</p>
                </div>
              ) : (
                <pre className="file-content">{fileContent}</pre>
              )}
            </div>
            <div className="modal-footer">
              <div className="file-info-modal">
                <span>Tamaño: {selectedFile.size} bytes</span>
                <span>Permisos: {selectedFile.permissions}</span>
                <span>Propietario: {selectedFile.owner}</span>
                <span>Grupo: {selectedFile.group}</span>
              </div>
              <button className="btn btn-secondary" onClick={closeModal}>
                Cerrar
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
};

export default FileExplorer;