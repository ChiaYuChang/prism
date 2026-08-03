import Alpine from "@alpinejs/csp";
import { controlRoom } from "./app.js";
import "./styles.css";

Alpine.data("controlRoom", controlRoom);
Alpine.start();
