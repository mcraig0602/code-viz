# Codebase Visualization Tool

This project provides a visualization of a codebase's dependencies, built using Go, React, and Cytoscape.js. It parses a Git repository, extracts file dependencies, and displays them as an interactive graph in a web browser.

## Features

-   Visualizes codebase dependencies as a graph.
-   Supports JavaScript and TypeScript codebases.
-   Uses React and Cytoscape.js for interactive graph rendering.
-   Parses Git history to generate visualizations for different commits.

## Requirements

-   Go: 1.21
-   Node.js: 18
-   npm: >=6

## Setup

1.  **Clone the repository:**

    ```sh
    git clone <repository-url>
    cd <repository-directory>
    ```

2.  **Install Go dependencies:**

    ```sh
    go mod download
    ```

3.  **Install React dependencies:**

    ```sh
    cd web
    npm install
    cd ..
    ```

## Usage

1.  **Run the parser:**

    ```sh
    go run cmd/parser/main.go --repo <path-to-codebase> --output data
    ```

    Replace `<path-to-codebase>` with the path to the Git repository you want to visualize. The `--output` flag specifies the directory where the generated JSON files will be stored.

2.  **Start the React development server:**

    ```sh
    cd web
    npm start
    ```

    This will start the React development server and open the visualization in your web browser.

## Configuration

The following configuration options are available for the parser:

-   `--repo`: Path to the codebase repository (default: `.`).
-   `--output`: Output directory for JSON files (default: `data`).
-   `--log-level`: Log level (debug, info, warn, error) (default: `info`).

## Development

### Building the React app

To build the React app for production, run:

```sh
cd web
npm run build
