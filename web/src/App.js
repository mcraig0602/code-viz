import React, { useState, useEffect } from 'react';
import CytoscapeComponent from 'react-cytoscapejs';
import pako from 'pako';
import axios from 'axios';
import './App.css';

function App() {
  const [commits, setCommits] = useState([]);
  const [selectedCommit, setSelectedCommit] = useState('');
  const [graph, setGraph] = useState([]);

  // Fetch commit list (dynamic in GitHub Pages)
  useEffect(() => {
    // Read commit list from a static file
    fetch('/commitList.json')
      .then(res => res.json())
      .then(data => {
        setCommits(data);
      })
      .catch(err => console.error('Error loading commits:', err));
  }, []);

  // Fetch and render graph
  useEffect(() => {
    if (!selectedCommit) return;
    fetch(`/data/${selectedCommit}.json.gz`)
      .then(res => res.arrayBuffer())
      .then(buffer => {
        const decompressed = pako.ungzip(new Uint8Array(buffer));
        const json = JSON.parse(new TextDecoder().decode(decompressed));
        const elements = [
          ...json.nodes.map(node => ({
            data: { id: node.id, label: node.id.split('/').pop(), language: node.language }
          })),
          ...json.edges.map(edge => ({
            data: { source: edge.source, target: edge.target }
          }))
        ];
        setGraph(elements);
      })
      .catch(err => console.error('Error loading graph:', err));
  }, [selectedCommit]);

  return (
    <div style={{ padding: '20px' }}>
      <h1>Codebase Visualization</h1>
      <select
        value={selectedCommit}
        onChange={e => setSelectedCommit(e.target.value)}
      >
        <option value="">Select a commit</option>
        {commits.map(commit => (
          <option key={commit} value={commit}>
            {commit}
          </option>
        ))}
      </select>
      <div style={{ height: '600px', border: '1px solid #ccc' }}>
        <CytoscapeComponent
          elements={graph}
          style={{ width: '100%', height: '100%' }}
          layout={{ name: 'cose' }}
          stylesheet={[
            {
              selector: 'node',
              style: {
                label: 'data(label)',
                fontSize: '12px',
                backgroundColor: 'data(language)',
                backgroundColorMap: {
                  js: '#f0db4f',
                  jsx: '#61dafb',
                  ts: '#3178c6',
                  tsx: '#3178c6'
                }
              }
            },
            {
              selector: 'edge',
              style: { width: 2, curveStyle: 'bezier', lineColor: '#ccc' }
            }
          ]}
        />
      </div>
    </div>
  );
}

export default App;