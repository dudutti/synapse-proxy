use std::ffi::CStr;
use std::os::raw::{c_char, c_int, c_void};
use std::path::Path;

use candle_core::{Device, Tensor, Result};
use candle_nn::VarBuilder;
use candle_transformers::models::bert::{BertModel, Config};
use tokenizers::Tokenizer;

pub struct EmbedderContext {
    model: BertModel,
    tokenizer: Tokenizer,
    device: Device,
}

#[no_mangle]
pub unsafe extern "C" fn rust_embedder_init(model_dir: *const c_char) -> *mut c_void {
    if model_dir.is_null() {
        return std::ptr::null_mut();
    }
    
    let c_str = CStr::from_ptr(model_dir);
    let dir_str = match c_str.to_str() {
        Ok(s) => s,
        Err(_) => return std::ptr::null_mut(),
    };
    
    let path = Path::new(dir_str);
    let config_path = path.join("config.json");
    let tokenizer_path = path.join("tokenizer.json");
    
    let weights_path = if path.join("model.safetensors").exists() {
        path.join("model.safetensors")
    } else if path.join("pytorch_model.bin").exists() {
        path.join("pytorch_model.bin")
    } else {
        return std::ptr::null_mut();
    };
    
    let device = Device::Cpu;
    
    let config_str = match std::fs::read_to_string(config_path) {
        Ok(s) => s,
        Err(_) => return std::ptr::null_mut(),
    };
    
    let config: Config = match serde_json::from_str(&config_str) {
        Ok(c) => c,
        Err(_) => return std::ptr::null_mut(),
    };
    
    let tokenizer = match Tokenizer::from_file(tokenizer_path) {
        Ok(t) => t,
        Err(_) => return std::ptr::null_mut(),
    };
    
    let vb = if weights_path.extension().map_or(false, |ext| ext == "safetensors") {
        match unsafe { VarBuilder::from_mmaped_safetensors(&[weights_path.clone()], candle_core::DType::F32, &device) } {
            Ok(v) => v,
            Err(_) => return std::ptr::null_mut(),
        }
    } else {
        match VarBuilder::from_pth(&weights_path, candle_core::DType::F32, &device) {
            Ok(v) => v,
            Err(_) => return std::ptr::null_mut(),
        }
    };
    
    let model = match BertModel::load(vb, &config) {
        Ok(m) => m,
        Err(_) => return std::ptr::null_mut(),
    };
    
    let ctx = Box::new(EmbedderContext {
        model,
        tokenizer,
        device,
    });
    
    Box::into_raw(ctx) as *mut c_void
}

#[no_mangle]
pub unsafe extern "C" fn rust_embedder_free(ctx: *mut c_void) {
    if !ctx.is_null() {
        let _ = Box::from_raw(ctx as *mut EmbedderContext);
    }
}

fn normalize_l2(v: &Tensor) -> Result<Tensor> {
    let norm = v.sqr()?.sum_keepdim(1)?.sqrt()?;
    v.broadcast_div(&norm)
}

#[no_mangle]
pub unsafe extern "C" fn rust_embedder_embed(
    ctx: *mut c_void,
    text: *const c_char,
    out_vector: *mut f32,
) -> c_int {
    if ctx.is_null() || text.is_null() || out_vector.is_null() {
        return -1;
    }
    
    let context = &*(ctx as *const EmbedderContext);
    
    let c_str = CStr::from_ptr(text);
    let text_str = match c_str.to_str() {
        Ok(s) => s,
        Err(_) => return -2,
    };
    
    let encoding = match context.tokenizer.encode(text_str, true) {
        Ok(e) => e,
        Err(_) => return -3,
    };
    
    let input_ids = encoding.get_ids();
    let attention_mask = encoding.get_attention_mask();
    
    let seq_len = input_ids.len();
    if seq_len == 0 {
        return -4;
    }
    
    let input_ids_tensor = match Tensor::new(input_ids, &context.device) {
        Ok(t) => match t.unsqueeze(0) {
            Ok(u) => u,
            Err(_) => return -5,
        },
        Err(_) => return -5,
    };
    
    let attention_mask_tensor = match Tensor::new(attention_mask, &context.device) {
        Ok(t) => match t.unsqueeze(0) {
            Ok(u) => u,
            Err(_) => return -51,
        },
        Err(_) => return -51,
    };
    
    let token_type_ids = match Tensor::zeros_like(&input_ids_tensor) {
        Ok(t) => t,
        Err(_) => return -6,
    };
    
    let token_embeddings = match context.model.forward(&input_ids_tensor, &token_type_ids, Some(&attention_mask_tensor)) {
        Ok(t) => t,
        Err(_) => return -7,
    };
    
    let mask_t = match attention_mask_tensor.to_dtype(token_embeddings.dtype()) {
        Ok(dt) => match dt.unsqueeze(2) {
            Ok(uu) => uu,
            Err(_) => return -8,
        },
        Err(_) => return -8,
    };
    
    let dims = token_embeddings.dims();
    let mask_broadcasted = match mask_t.broadcast_as(dims) {
        Ok(t) => t,
        Err(_) => return -9,
    };
    
    let masked_embeddings = match token_embeddings.broadcast_mul(&mask_broadcasted) {
        Ok(t) => t,
        Err(_) => return -10,
    };
    
    let sum_embeddings = match masked_embeddings.sum(1) {
        Ok(t) => t,
        Err(_) => return -11,
    };
    
    let sum_mask = match mask_broadcasted.sum(1) {
        Ok(t) => match t.clamp(1e-9, f64::INFINITY) {
            Ok(c) => c,
            Err(_) => return -12,
        },
        Err(_) => return -12,
    };
    
    let mean_pooled = match sum_embeddings.broadcast_div(&sum_mask) {
        Ok(t) => t,
        Err(_) => return -13,
    };
    
    let normalized = match normalize_l2(&mean_pooled) {
        Ok(t) => t,
        Err(_) => return -14,
    };
    
    let normalized = match normalized.squeeze(0) {
        Ok(t) => t,
        Err(_) => return -15,
    };
    
    let vec_data = match normalized.to_vec1::<f32>() {
        Ok(v) => v,
        Err(_) => return -16,
    };
    
    if vec_data.len() != 384 {
        return -17;
    }
    
    std::ptr::copy_nonoverlapping(vec_data.as_ptr(), out_vector, 384);
    
    0
}
