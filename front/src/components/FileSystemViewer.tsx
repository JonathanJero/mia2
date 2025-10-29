import React, { useState, useEffect } from 'react';
import DiskSelector from './DiskSelector';
import PartitionViewer from './PartitionViewer';
import FileExplorer from './FileExplorer';
import '../App.css';
import { BACKEND_URL } from '../config';

interface FileSystemViewerProps {
  session: {
    partitionId: string;
    username: string;
    isLoggedIn: boolean;
    isRoot: boolean;
  };
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

const FileSystemViewer: React.FC<FileSystemViewerProps> = ({ session, onClose }) => {
  const [disks, setDisks] = useState<Disk[]>([]);
  const [selectedDisk, setSelectedDisk] = useState<Disk | null>(null);
  const [selectedPartition, setSelectedPartition] = useState<Partition | null>(null);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState('');

  // BACKEND_URL provided by src/config.ts

  useEffect(() => {
    loadDisks();
  }, []);

  const loadDisks = async () => {
    setIsLoading(true);
    setError('');

    try {
      const response = await fetch(`${BACKEND_URL}/disks`);
      
      if (response.ok) {
        const data = await response.json();
        setDisks(data.disks || []);
      } else {
        setError('Error al cargar los discos');
      }
    } catch (err) {
      setError('Error de conexión con el servidor');
    } finally {
      setIsLoading(false);
    }
  };

  const handleDiskSelect = (disk: Disk) => {
    setSelectedDisk(disk);
    setSelectedPartition(null);
  };

  const handlePartitionSelect = (partition: Partition) => {
    setSelectedPartition(partition);
  };

  const handleBack = () => {
    if (selectedPartition) {
      setSelectedPartition(null);
    } else {
      setSelectedDisk(null);
    }
  };

  const hasAccessToPartition = (partition: Partition): boolean => {
    // Root tiene acceso a todo
    if (session.isRoot) return true;
    
    // La partición debe estar montada
    if (!partition.isMounted) return false;
    
    // El usuario debe estar logueado en esa partición
    return partition.id === session.partitionId;
  };

  return (
    <div className="viewer-overlay" onClick={onClose}>
      <div className="viewer-container" onClick={(e) => e.stopPropagation()}>
        <div className="viewer-header">
          <div className="viewer-title">
            <h2>
              <span className="viewer-icon">💾</span>
              Visualizador del Sistema de Archivos
            </h2>
            {session.isLoggedIn && (
              <div className="viewer-session-info">
                <span className="session-badge">
                  {session.isRoot ? '🔑' : '👤'} {session.username}
                </span>
                <span className="session-partition">
                  📁 {session.partitionId}
                </span>
              </div>
            )}
          </div>
          <button className="viewer-close" onClick={onClose}>×</button>
        </div>

        {/* Breadcrumb Navigation */}
        {(selectedDisk || selectedPartition) && (
          <div className="viewer-breadcrumb">
            <button 
              className="breadcrumb-item"
              onClick={() => {
                setSelectedDisk(null);
                setSelectedPartition(null);
              }}
            >
              💾 Discos
            </button>
            {selectedDisk && (
              <>
                <span className="breadcrumb-separator">›</span>
                <button 
                  className="breadcrumb-item"
                  onClick={() => setSelectedPartition(null)}
                >
                  📀 {selectedDisk.path.split('/').pop()}
                </button>
              </>
            )}
            {selectedPartition && (
              <>
                <span className="breadcrumb-separator">›</span>
                <span className="breadcrumb-item active">
                  📁 {selectedPartition.name}
                </span>
              </>
            )}
          </div>
        )}

        <div className="viewer-body">
          {isLoading ? (
            <div className="viewer-loading">
              <div className="loader"></div>
              <p>Cargando discos...</p>
            </div>
          ) : error ? (
            <div className="viewer-error">
              <span className="error-icon">⚠️</span>
              <p>{error}</p>
              <button className="btn btn-primary" onClick={loadDisks}>
                Reintentar
              </button>
            </div>
          ) : !selectedDisk ? (
            <DiskSelector 
              disks={disks}
              onSelectDisk={handleDiskSelect}
            />
          ) : !selectedPartition ? (
            <PartitionViewer
              disk={selectedDisk}
              session={session}
              onSelectPartition={handlePartitionSelect}
              hasAccessToPartition={hasAccessToPartition}
              onBack={handleBack}
            />
          ) : (
            <FileExplorer
              partition={selectedPartition}
              session={session}
              onBack={handleBack}
            />
          )}
        </div>
      </div>
    </div>
  );
};

export default FileSystemViewer;